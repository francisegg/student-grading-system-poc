package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template" // ★ 新增這個 import，為了使用 template.FuncMap
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
	// 全域變數：從環境變數讀取
	CurrentSubject string // 例如 "circuit", "antenna"
	IsAdminMode    bool   // 是否為老師總後台
	AppName        string // 網站標題
)

const TotalScoreColName = "Total learning-progress points"

// 檢查是否為老師 (白名單)
func isTeacher(email string) bool {
	whitelist := os.Getenv("TEACHER_WHITELIST")
	return strings.Contains(whitelist, email)
}

// GORM Scope: 自動過濾科目
// 如果有設定 CurrentSubject，所有 DB 查詢都會自動加上 WHERE subject = '...'
func filterSubject(db *gorm.DB) *gorm.DB {
	if CurrentSubject != "" {
		return db.Where("subject = ?", CurrentSubject)
	}
	return db
}

func init() {
	if err := godotenv.Load(); err != nil {
		log.Println("找不到 .env 檔案，使用系統環境變數")
	}

	// 1. 初始化全域設定
	CurrentSubject = os.Getenv("APP_SUBJECT")
	AppName = os.Getenv("APP_NAME")
	if AppName == "" {
		AppName = "學生分數平台"
	}
	if os.Getenv("APP_MODE") == "admin" {
		IsAdminMode = true
		AppName = "教師總管理後台"
	}

	// 2. 資料庫連線
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Taipei",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
	var err error
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("資料庫連線失敗: ", err)
	}

	// 自動遷移 Schema (加入 Roster)
	db.AutoMigrate(&models.Student{}, &models.Grade{}, &models.Roster{})

	// 3. Google OAuth 設定
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

	// ★★★ 關鍵修正：註冊 inc 函式 ★★★
	// 這一段必須放在 LoadHTMLGlob 之前
	r.SetFuncMap(template.FuncMap{
		"inc": func(i int) int {
			return i + 1
		},
	})

	store := cookie.NewStore([]byte(os.Getenv("SESSION_SECRET")))
	r.Use(sessions.Sessions("mysession", store))
	r.LoadHTMLGlob("templates/*")

	// --- 1. 首頁 (分流邏輯) ---
	r.GET("/", func(c *gin.Context) {
		session := sessions.Default(c)
		uid := session.Get("user_id") // ★ 這一行非常重要

		// 【情境 A：管理員模式】
		if IsAdminMode {
			if uid == nil {
				// 未登入 -> 顯示登入頁
				c.HTML(http.StatusOK, "index.html", gin.H{"Logged": false, "AppName": AppName, "IsAdminMode": true})
				return
			}
			// 已登入 -> 顯示科目選擇頁 (Admin Dashboard)
			var subjects []string
			// 1. 先嘗試從資料庫找出有成績的科目
			db.Model(&models.Grade{}).Distinct("subject").Pluck("subject", &subjects)

			// 2. 手動補上預設科目
			knownSubjects := []string{"circuit", "antenna"}

			// 去重複合併
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

			// 取得使用者 Email
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

		// 【情境 B：學生/單一科目模式】
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

	// --- 2. 登入/登出 (共用) ---
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

		// 【修正：情境 A：管理員模式登入】
		if IsAdminMode {
			// 1. 檢查是否為老師
			if !isTeacher(gUser.Email) {
				c.String(403, "🚫 抱歉，只有老師可以登入此後台。")
				return
			}
			// 2. 老師登入成功，設定 Session
			session.Set("user_id", "ADMIN_"+gUser.Email)
			session.Save()

			// 3. 導回首頁 (由首頁負責顯示 Dashboard)
			c.Redirect(http.StatusSeeOther, "/")
			return
		}

		// 【情境 B：學生模式登入】
		var s models.Student
		// 查詢時加上科目過濾
		result := db.Scopes(filterSubject).Where("email = ?", gUser.Email).First(&s)

		if result.Error == gorm.ErrRecordNotFound || s.StudentID == "" {
			// 沒資料 -> 去註冊
			session.Set("temp_email", gUser.Email)
			session.Set("temp_name", gUser.Name)
			session.Save()
			c.Redirect(http.StatusSeeOther, "/register")
			return
		}

		// 登入成功
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

	// --- 3. 註冊 (學生模式專用) ---
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
		googleName := session.Get("temp_name") // 從 Google 取得的名字

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

		// 1. 檢查名單 (Roster) 是否有這個學號
		var roster models.Roster
		if err := db.Where("student_id = ? AND subject = ?", inputID, CurrentSubject).First(&roster).Error; err != nil {
			c.String(400, "❌ 驗證失敗：此學號不在名單中，請檢查輸入。")
			return
		}

		// 2. 檢查該學號是否已經被註冊
		var existStudent models.Student
		if err := db.Scopes(filterSubject).Where("student_id = ?", inputID).First(&existStudent).Error; err == nil {
			c.String(400, "❌ 綁定失敗：此學號已經被註冊過了！")
			return
		}

		// 3. 建立學生帳號
		newStudent := models.Student{
			Email:     userEmail,
			Name:      userName,     // 使用 Google 名字
			StudentID: roster.StudentID,
			Class:     roster.Class, // ★ 自動從 Roster 帶入班級
			Subject:   CurrentSubject,
		}

		if err := db.Create(&newStudent).Error; err != nil {
			c.String(500, "資料庫寫入失敗")
			return
		}

		// 4. 註冊成功
		session.Set("user_id", newStudent.ID)
		session.Delete("temp_email")
		session.Delete("temp_name")
		session.Save()
		c.Redirect(302, "/")
	})

	// --- 4. 查詢成績 (學生模式專用) ---
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
		// 注意：這裡是檢查整個科目 (CurrentSubject) 有沒有成績，而不只是該位學生
		db.Model(&models.Grade{}).Where("subject = ?", CurrentSubject).Count(&globalGradeCount)

		if globalGradeCount == 0 {
			// 如果完全沒成績，直接顯示「尚未開放」頁面，避免後續計算報錯
			c.HTML(http.StatusOK, "no_grades.html", gin.H{
				"User":    s,
				"AppName": AppName,
				"Subject": CurrentSubject,
			})
			return
		}

		// 成績查詢邏輯...
		var displayGrades []models.Grade
		db.Scopes(filterSubject).Where("student_id = ? AND item_name != ?", s.StudentID, TotalScoreColName).Order("id asc").Find(&displayGrades)

		var myTotalGrade models.Grade
		var classTotals []float64

		type Result struct {
			StudentID string
			Score     float64
		}
		var results []Result

		query := db.Table("grades").
			Select("grades.student_id, grades.score").
			Joins("JOIN students ON students.student_id = grades.student_id").
			Where("grades.item_name = ?", TotalScoreColName).
			Where("grades.subject = ?", CurrentSubject).
			Where("students.subject = ?", CurrentSubject).
			Where("students.class = ?", s.Class) // ★ 修正為 Class

		query.Scan(&results)

		if len(results) == 0 {
			db.Table("grades").
				Select("grades.student_id, SUM(grades.score) as total").
				Joins("JOIN students ON students.student_id = grades.student_id").
				Where("grades.subject = ?", CurrentSubject).
				Where("students.subject = ?", CurrentSubject).
				Where("students.class = ?", s.Class). // ★ 修正為 Class
				Group("grades.student_id").
				Scan(&results)
		}

		for _, r := range results {
			classTotals = append(classTotals, r.Score)
			if r.StudentID == s.StudentID {
				myTotalGrade.Score = r.Score
			}
		}
		myTotal := myTotalGrade.Score

		// 統計計算
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

		// Top 3
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

	// --- 5. 老師後台功能 ---
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

		// ★ 新增功能：查詢名單與註冊狀態
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

	teacher.POST("/upload", func(c *gin.Context) {
		targetSubject := CurrentSubject
		if IsAdminMode {
			targetSubject = c.PostForm("subject")
		}

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

		header := records[0]
		idIndex := -1
		for i, colName := range header {
			if strings.EqualFold(strings.TrimSpace(colName), "ID") {
				idIndex = i
				break
			}
		}
		if idIndex == -1 {
			c.String(400, "❌ 找不到 'ID' 欄位")
			return
		}

		ignoreCols := map[string]bool{"No.": true, "Class": true, "ID": true, "Grade": true, "Weight of final exam (%)": true}

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

			for colIdx, cellValue := range row {
				colName := strings.TrimSpace(header[colIdx])
				if ignoreCols[colName] {
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
			}
		}

		redirectUrl := "/teacher/dashboard"
		if IsAdminMode {
			redirectUrl += "?subject=" + targetSubject
		}
		c.Redirect(http.StatusSeeOther, redirectUrl)
	})

	// 📌 修改：上傳修課名單 (支援 No., Class, ID 格式)
	teacher.POST("/upload-roster", func(c *gin.Context) {
		targetSubject := CurrentSubject
		if IsAdminMode {
			targetSubject = c.PostForm("subject")
		}

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

		successCount := 0
		for i, row := range records {
			if i == 0 {
				continue
			} // 跳過標題列
			if len(row) < 3 {
				continue
			} // 確保欄位足夠

			// 解析 CSV 欄位
			class := strings.TrimSpace(row[1]) // 第二欄是班級
			sid := strings.TrimSpace(row[2])   // 第三欄是學號

			if sid == "" {
				continue
			}

			// 寫入 Roster 表
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

		log.Printf("成功匯入 %d 筆名單", successCount)

		// 導回 Dashboard
		redirectUrl := "/teacher/dashboard"
		if IsAdminMode {
			redirectUrl += "?subject=" + targetSubject
		}
		c.Redirect(http.StatusSeeOther, redirectUrl)
	})

	// 【新增：一鍵清空功能】
	teacher.POST("/delete-all", func(c *gin.Context) {
		targetSubject := CurrentSubject
		if IsAdminMode {
			targetSubject = c.PostForm("subject")
		}

		// 刪除該科目的所有成績
		if err := db.Where("subject = ?", targetSubject).Delete(&models.Grade{}).Error; err != nil {
			c.String(500, "刪除失敗")
			return
		}

		// 導回 Dashboard
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