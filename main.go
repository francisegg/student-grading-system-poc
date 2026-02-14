package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"grade-system/models"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	db                *gorm.DB
	googleOauthConfig *oauth2.Config
	CurrentSubject    string
	IsAdminMode       bool
	AppName           string
)

const TotalScoreColName = "Total learning-progress points"

func isTeacher(email string) bool {
	whitelist := os.Getenv("TEACHER_WHITELIST")
	return strings.Contains(whitelist, email)
}

func filterSubject(db *gorm.DB) *gorm.DB {
	if CurrentSubject != "" {
		return db.Where("subject = ?", CurrentSubject)
	}
	return db
}

// 輔助函式：清理 BOM 與空白
func cleanHeader(h string) string {
	h = strings.ReplaceAll(h, "\ufeff", "")
	return strings.TrimSpace(h)
}

func init() {
	if err := godotenv.Load(); err != nil {
		log.Println("找不到 .env 檔案，使用系統環境變數")
	}

	CurrentSubject = os.Getenv("APP_SUBJECT")
	AppName = os.Getenv("APP_NAME")
	if AppName == "" {
		AppName = "學生分數平台"
	}
	if os.Getenv("APP_MODE") == "admin" {
		IsAdminMode = true
		AppName = "教師總管理後台"
	}

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Taipei",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
	var err error
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("資料庫連線失敗: ", err)
	}

	db.AutoMigrate(&models.Student{}, &models.Grade{}, &models.Roster{})

	googleOauthConfig = &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("GOOGLE_REDIRECT_URL"),
		Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile"},
		Endpoint:     google.Endpoint,
	}
}

func main() {
	r := gin.Default()

	r.SetFuncMap(template.FuncMap{
		"inc": func(i int) int {
			return i + 1
		},
	})

	store := cookie.NewStore([]byte(os.Getenv("SESSION_SECRET")))
	r.Use(sessions.Sessions("mysession", store))
	r.LoadHTMLGlob("templates/*")

	// --- 1. 首頁 ---
	r.GET("/", func(c *gin.Context) {
		session := sessions.Default(c)
		uid := session.Get("user_id")

		if IsAdminMode {
			if uid == nil {
				c.HTML(http.StatusOK, "index.html", gin.H{"Logged": false, "AppName": AppName, "IsAdminMode": true})
				return
			}
			var subjects []string
			db.Model(&models.Grade{}).Distinct("subject").Pluck("subject", &subjects)

			knownSubjects := []string{"circuit", "antenna"}
			subjectMap := make(map[string]bool)
			for _, s := range subjects {
				subjectMap[s] = true
			}
			for _, s := range knownSubjects {
				subjectMap[s] = true
			}

			var finalSubjects []string
			for s := range subjectMap {
				finalSubjects = append(finalSubjects, s)
			}
			sort.Strings(finalSubjects)

			userEmail := ""
			if uStr, ok := uid.(string); ok {
				userEmail = strings.TrimPrefix(uStr, "ADMIN_")
			}

			c.HTML(http.StatusOK, "admin_dashboard.html", gin.H{
				"Subjects":  finalSubjects,
				"AppName":   AppName,
				"UserEmail": userEmail,
			})
			return
		}

		if uid == nil {
			c.HTML(http.StatusOK, "index.html", gin.H{"Logged": false, "AppName": AppName})
			return
		}

		var s models.Student
		result := db.Scopes(filterSubject).First(&s, uid)

		if result.Error != nil {
			c.Redirect(http.StatusSeeOther, "/logout")
			return
		}

		c.HTML(http.StatusOK, "index.html", gin.H{
			"Logged":    true,
			"User":      s,
			"IsTeacher": isTeacher(s.Email),
			"AppName":   AppName,
		})
	})

	// --- 2. 登入/登出 ---
	r.GET("/login", func(c *gin.Context) {
		url := googleOauthConfig.AuthCodeURL("state")
		c.Redirect(http.StatusTemporaryRedirect, url)
	})

	r.GET("/auth/callback", func(c *gin.Context) {
		token, err := googleOauthConfig.Exchange(context.Background(), c.Query("code"))
		if err != nil {
			c.Redirect(302, "/")
			return
		}

		resp, _ := http.Get("https://www.googleapis.com/oauth2/v2/userinfo?access_token=" + token.AccessToken)
		defer resp.Body.Close()
		data, _ := ioutil.ReadAll(resp.Body)

		var gUser struct{ Email, Name string }
		json.Unmarshal(data, &gUser)
		session := sessions.Default(c)

		if IsAdminMode {
			if !isTeacher(gUser.Email) {
				c.String(403, "🚫 抱歉，只有老師可以登入此後台。")
				return
			}
			session.Set("user_id", "ADMIN_"+gUser.Email)
			session.Save()
			c.Redirect(http.StatusSeeOther, "/")
			return
		}

		var s models.Student
		result := db.Scopes(filterSubject).Where("email = ?", gUser.Email).First(&s)

		if result.Error == gorm.ErrRecordNotFound || s.StudentID == "" {
			session.Set("temp_email", gUser.Email)
			session.Set("temp_name", gUser.Name)
			session.Save()
			c.Redirect(http.StatusSeeOther, "/register")
			return
		}

		session.Set("user_id", s.ID)
		session.Save()
		c.Redirect(http.StatusSeeOther, "/")
	})

	r.GET("/logout", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Clear()
		session.Save()
		c.Redirect(302, "/")
	})

	// --- 3. 註冊 ---
	r.GET("/register", func(c *gin.Context) {
		if IsAdminMode {
			c.Redirect(302, "/")
			return
		}
		session := sessions.Default(c)
		email := session.Get("temp_email")
		if email == nil {
			c.Redirect(302, "/")
			return
		}
		c.HTML(200, "register.html", gin.H{"Email": email})
	})

	r.POST("/register", func(c *gin.Context) {
		session := sessions.Default(c)
		email := session.Get("temp_email")
		googleName := session.Get("temp_name")

		if email == nil {
			c.Redirect(302, "/")
			return
		}
		userEmail := email.(string)
		userName := ""
		if googleName != nil {
			userName = googleName.(string)
		}

		inputID := strings.TrimSpace(c.PostForm("student_id"))

		var roster models.Roster
		if err := db.Where("student_id = ? AND subject = ?", inputID, CurrentSubject).First(&roster).Error; err != nil {
			c.String(400, "❌ 驗證失敗：此學號不在名單中，請檢查輸入。")
			return
		}

		var existStudent models.Student
		if err := db.Scopes(filterSubject).Where("student_id = ?", inputID).First(&existStudent).Error; err == nil {
			c.String(400, "❌ 綁定失敗：此學號已經被註冊過了！")
			return
		}

		newStudent := models.Student{
			Email:     userEmail,
			Name:      userName,
			StudentID: roster.StudentID,
			Class:     roster.Class,
			Subject:   CurrentSubject,
		}

		if err := db.Create(&newStudent).Error; err != nil {
			c.String(500, "資料庫寫入失敗")
			return
		}

		session.Set("user_id", newStudent.ID)
		session.Delete("temp_email")
		session.Delete("temp_name")
		session.Save()
		c.Redirect(302, "/")
	})

	// --- 4. 查詢成績 ---
	r.GET("/my-grades", func(c *gin.Context) {
		if IsAdminMode {
			c.Redirect(302, "/")
			return
		}

		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil {
			c.Redirect(302, "/")
			return
		}

		var s models.Student
		db.Scopes(filterSubject).First(&s, uid)

		var globalGradeCount int64
		db.Model(&models.Grade{}).Where("subject = ?", CurrentSubject).Count(&globalGradeCount)

		if globalGradeCount == 0 {
			c.HTML(http.StatusOK, "no_grades.html", gin.H{
				"User":    s,
				"AppName": AppName,
				"Subject": CurrentSubject,
			})
			return
		}

		var displayGrades []models.Grade
		db.Scopes(filterSubject).
			Where("student_id = ?", s.StudentID).
			Where("item_name NOT IN ?", []string{TotalScoreColName, "No.", "No"}).
			Order("id asc").
			Find(&displayGrades)

		var myTotalGrade models.Grade
		var classTotals []float64

		type Result struct {
			StudentID string
			Score     float64
		}
		var results []Result

		db.Table("grades").
			Select("grades.student_id, SUM(grades.score) as score").
			Joins("JOIN students ON students.student_id = grades.student_id").
			Where("grades.subject = ?", CurrentSubject).
			Where("grades.item_name NOT IN ?", []string{"No.", "No"}).
			Where("students.subject = ?", CurrentSubject).
			Where("students.class = ?", s.Class).
			Group("grades.student_id").
			Scan(&results)

		for _, r := range results {
			classTotals = append(classTotals, r.Score)
			if r.StudentID == s.StudentID {
				myTotalGrade.Score = r.Score
			}
		}
		myTotal := myTotalGrade.Score

		sum := 0.0
		minScore, maxScore := 1000.0, -1.0
		for _, t := range classTotals {
			sum += t
			if t < minScore {
				minScore = t
			}
			if t > maxScore {
				maxScore = t
			}
		}
		if len(classTotals) == 0 {
			minScore, maxScore = 0, 0
		}

		mean := 0.0
		if len(classTotals) > 0 {
			mean = sum / float64(len(classTotals))
		}

		varianceSum := 0.0
		for _, t := range classTotals {
			varianceSum += math.Pow(t-mean, 2)
		}
		stdDev := 0.0
		if len(classTotals) > 0 {
			stdDev = math.Sqrt(varianceSum / float64(len(classTotals)))
		}

		sort.Float64s(classTotals)
		rank := 0
		for i, t := range classTotals {
			if t >= myTotal {
				rank = i
				break
			}
			rank = i + 1
		}
		percentile := 0.0
		if len(classTotals) > 1 {
			percentile = (float64(rank) / float64(len(classTotals))) * 100
		} else if len(classTotals) == 1 {
			percentile = 100
		}

		var top3 []float64
		count := len(classTotals)
		for i := count - 1; i >= 0 && len(top3) < 3; i-- {
			top3 = append(top3, classTotals[i])
		}
		finalWeight := 100.0 - myTotal
		if finalWeight < 0 {
			finalWeight = 0
		}

		c.HTML(200, "my_grades.html", gin.H{
			"User":        s,
			"Grades":      displayGrades,
			"MyTotal":     myTotal,
			"ClassMean":   mean,
			"ClassStdDev": stdDev,
			"ClassMin":    minScore,
			"ClassMax":    maxScore,
			"Percentile":  int(percentile),
			"Top3":        top3,
			"FinalWeight": finalWeight,
			"AppName":     AppName,
		})
	})

	teacher := r.Group("/teacher")
	teacher.Use(func(c *gin.Context) {
		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil {
			c.Redirect(302, "/")
			c.Abort()
			return
		}
		isAdminSession := strings.HasPrefix(fmt.Sprintf("%v", uid), "ADMIN_")
		if !isAdminSession {
			var s models.Student
			if err := db.Scopes(filterSubject).First(&s, uid).Error; err != nil || !isTeacher(s.Email) {
				c.String(403, "🚫 權限不足")
				c.Abort()
				return
			}
		}
		c.Next()
	})

	teacher.GET("/dashboard", func(c *gin.Context) {
		targetSubject := CurrentSubject
		if IsAdminMode {
			targetSubject = c.Query("subject")
			if targetSubject == "" {
				c.Redirect(302, "/")
				return
			}
		}

		var allGrades []models.Grade
		db.Where("subject = ?", targetSubject).Order("created_at desc").Find(&allGrades)

		type RosterRow struct {
			Class     string
			StudentID string
			Name      string
			Email     string
		}
		var rosterRows []RosterRow

		db.Table("rosters").
			Select("rosters.class, rosters.student_id, rosters.name, students.email").
			Joins("LEFT JOIN students ON students.student_id = rosters.student_id").
			Where("rosters.subject = ?", targetSubject).
			Order("rosters.class ASC, rosters.student_id ASC").
			Scan(&rosterRows)

		c.HTML(200, "teacher.html", gin.H{
			"AllGrades":   allGrades,
			"RosterList":  rosterRows,
			"Subject":     targetSubject,
			"AppName":     AppName,
			"IsAdmin":     IsAdminMode,
		})
	})

	// 📌 上傳成績
	teacher.POST("/upload", func(c *gin.Context) {
		targetSubject := CurrentSubject
		if IsAdminMode {
			targetSubject = c.PostForm("subject")
		}

		log.Println("--- 開始上傳成績 ---")
		file, _ := c.FormFile("csv_file")
		if file == nil {
			c.String(400, "❌ 請選擇檔案")
			return
		}
		f, _ := file.Open()
		defer f.Close()

		reader := csv.NewReader(f)
		reader.FieldsPerRecord = -1
		records, err := reader.ReadAll()
		if err != nil {
			c.String(400, "CSV 讀取失敗")
			return
		}
		if len(records) < 2 {
			c.String(400, "無數據")
			return
		}

		// 取得白名單 Map
		var validStudentIDs []string
		db.Model(&models.Roster{}).Where("subject = ?", targetSubject).Pluck("student_id", &validStudentIDs)
		
		validStudentMap := make(map[string]bool)
		for _, id := range validStudentIDs {
			validStudentMap[id] = true
		}

		header := records[0]
		// 強制去除 BOM
		if len(header) > 0 {
			header[0] = strings.TrimPrefix(header[0], "\ufeff")
		}

		idIndex := -1
		for i, colName := range header {
			cleanName := strings.ToLower(cleanHeader(colName))
			if cleanName == "id" || cleanName == "student id" || cleanName == "student_id" || cleanName == "學號" {
				idIndex = i
				break
			}
		}
		
		if idIndex == -1 {
			log.Printf("❌ 錯誤: CSV 標題列找不到 ID 欄位。讀到的標題: %v", header)
			c.String(400, fmt.Sprintf("❌ 找不到 'ID' 欄位，請檢查 CSV 標題。偵測到的標題: %v", header))
			return
		}

		ignoreCols := map[string]bool{
			"No.": true, "No": true, "class": true, "id": true, "grade": true,
			"weight of final exam (%)": true,
		}

		successCount := 0
		skippedCount := 0

		for i, row := range records {
			if i == 0 {
				continue
			}
			if len(row) <= idIndex {
				continue
			}
			studentID := strings.TrimSpace(row[idIndex])
			if studentID == "" {
				continue
			}

			// 白名單檢查
			if !validStudentMap[studentID] {
				skippedCount++
				continue
			}

			for colIdx, cellValue := range row {
				colName := cleanHeader(header[colIdx])
				if ignoreCols[colName] || ignoreCols[strings.ToLower(colName)] {
					continue
				}

				var score float64
				cellValue = strings.TrimSpace(cellValue)
				if cellValue == "" || strings.EqualFold(cellValue, "NaN") {
					continue
				}
				if s, err := strconv.ParseFloat(cellValue, 64); err == nil {
					score = s
				} else {
					score = 0
				}

				db.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "student_id"}, {Name: "item_name"}, {Name: "subject"}},
					DoUpdates: clause.AssignmentColumns([]string{"score", "updated_at"}),
				}).Create(&models.Grade{
					StudentID: studentID,
					ItemName:  colName,
					Score:     score,
					Subject:   targetSubject,
				})
				successCount++
			}
		}
		
		log.Printf("匯入完成。寫入 %d 筆，略過 %d 筆。", successCount, skippedCount)

		redirectUrl := "/teacher/dashboard"
		if IsAdminMode {
			redirectUrl += "?subject=" + targetSubject
		}
		c.Redirect(http.StatusSeeOther, redirectUrl)
	})

	// 📌 上傳名單 (改良版：支援 BOM 移除與欄位搜尋)
	teacher.POST("/upload-roster", func(c *gin.Context) {
		targetSubject := CurrentSubject
		if IsAdminMode {
			targetSubject = c.PostForm("subject")
		}

		log.Println("--- 開始上傳名單 ---")
		file, _ := c.FormFile("roster_file")
		if file == nil {
			c.String(400, "❌ 請選擇檔案")
			return
		}
		f, _ := file.Open()
		defer f.Close()

		reader := csv.NewReader(f)
		records, err := reader.ReadAll()
		if err != nil {
			c.String(400, "CSV 讀取失敗")
			return
		}

		// 嘗試自動定位 ID 與 Class 欄位
		// 預設: No(0), Class(1), ID(2)
		classIndex := 1
		idIndex := 2

		header := records[0]
		if len(header) > 0 {
			header[0] = strings.TrimPrefix(header[0], "\ufeff") // 去除 BOM
			
			// 如果第一欄就是 ID (沒有 No.)
			firstCol := strings.ToLower(cleanHeader(header[0]))
			if firstCol == "id" || firstCol == "學號" || firstCol == "student_id" {
				// 假設格式: ID, Class, Name 或 ID, Name, Class... 比較難猜，但嘗試基本款
				// 使用者回報格式為: ID, ... (可能手動改過)
				// 這裡維持原有的 1, 2 預設值，但針對 No, Class, ID 優化
				// 如果欄位數少於 3，可能要重新判斷
			}
			
			// 進階搜尋
			for i, col := range header {
				cName := strings.ToLower(cleanHeader(col))
				if cName == "class" || cName == "班級" {
					classIndex = i
				}
				if cName == "id" || cName == "學號" || cName == "student_id" {
					idIndex = i
				}
			}
		}

		successCount := 0
		for i, row := range records {
			if i == 0 {
				continue
			}
			if len(row) <= idIndex || len(row) <= classIndex {
				continue
			}

			class := strings.TrimSpace(row[classIndex])
			sid := strings.TrimSpace(row[idIndex])

			if sid == "" {
				continue
			}

			db.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "student_id"}, {Name: "subject"}},
				DoUpdates: clause.AssignmentColumns([]string{"class", "updated_at"}),
			}).Create(&models.Roster{
				StudentID: sid,
				Class:     class,
				Subject:   targetSubject,
			})
			successCount++
		}

		log.Printf("成功匯入 %d 筆名單資料。", successCount)

		redirectUrl := "/teacher/dashboard"
		if IsAdminMode {
			redirectUrl += "?subject=" + targetSubject
		}
		c.Redirect(http.StatusSeeOther, redirectUrl)
	})

// ★★★ 修正：加入 Unscoped() 進行物理刪除 ★★★
	teacher.POST("/delete-roster", func(c *gin.Context) {
		targetSubject := CurrentSubject
		if IsAdminMode {
			targetSubject = c.PostForm("subject")
		}

		log.Printf("🗑️ 正在 [物理清空] %s 的修課名單...", targetSubject)
		
		// 注意這裡加了 .Unscoped()
		if err := db.Unscoped().Where("subject = ?", targetSubject).Delete(&models.Roster{}).Error; err != nil {
			c.String(500, "刪除失敗")
			return
		}

		redirectUrl := "/teacher/dashboard"
		if IsAdminMode {
			redirectUrl += "?subject=" + targetSubject
		}
		c.Redirect(http.StatusSeeOther, redirectUrl)
	})

// ★★★ 修正：加入 Unscoped() 進行物理刪除 ★★★
	teacher.POST("/delete-all", func(c *gin.Context) {
		targetSubject := CurrentSubject
		if IsAdminMode {
			targetSubject = c.PostForm("subject")
		}

		log.Printf("🗑️ 正在 [物理清空] %s 的所有成績...", targetSubject)

		// 注意這裡加了 .Unscoped()
		if err := db.Unscoped().Where("subject = ?", targetSubject).Delete(&models.Grade{}).Error; err != nil {
			c.String(500, "刪除失敗")
			return
		}

		redirectUrl := "/teacher/dashboard"
		if IsAdminMode {
			redirectUrl += "?subject=" + targetSubject
		}
		c.Redirect(http.StatusSeeOther, redirectUrl)
	})

	teacher.POST("/delete/:id", func(c *gin.Context) {
		id := c.Param("id")
		db.Scopes(filterSubject).Unscoped().Delete(&models.Grade{}, id)
		c.Redirect(http.StatusSeeOther, c.Request.Header.Get("Referer"))
	})

	r.Run(":8080")
}