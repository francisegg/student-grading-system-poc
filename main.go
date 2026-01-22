package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
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
	
	// 自動遷移 Schema (請確保 models/schema.go 已經加上 Subject 欄位)
	db.AutoMigrate(&models.Student{}, &models.Grade{})

	// 3. Google OAuth 設定
	googleOauthConfig = &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("GOOGLE_REDIRECT_URL"), // 從 .env 讀取，因為不同子網域不同
		Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile"},
		Endpoint:     google.Endpoint,
	}
}

func main() {
	r := gin.Default()
	store := cookie.NewStore([]byte(os.Getenv("SESSION_SECRET")))
	// 設定 Cookie Domain 讓子網域可以共用 (如果需要)
	// store.Options(sessions.Options{Path: "/", Domain: ".teaegg.space", MaxAge: 86400 * 7, HttpOnly: true, Secure: true})
	r.Use(sessions.Sessions("mysession", store))
	r.LoadHTMLGlob("templates/*")

	// --- 1. 首頁 (分流邏輯) ---
	r.GET("/", func(c *gin.Context) {
		session := sessions.Default(c)
		uid := session.Get("user_id")

		// 【情境 A：管理員模式】
		if IsAdminMode {
			if uid == nil {
				// 未登入 -> 顯示登入頁
				c.HTML(http.StatusOK, "index.html", gin.H{"Logged": false, "AppName": AppName, "IsAdminMode": true})
				return
			}
			// 已登入 -> 顯示科目選擇頁 (Admin Dashboard)
			// 找出目前資料庫裡所有不重複的科目
			var subjects []string
			db.Model(&models.Grade{}).Distinct("subject").Pluck("subject", &subjects)
			c.HTML(http.StatusOK, "admin_dashboard.html", gin.H{
				"Subjects": subjects,
				"AppName":  AppName,
			})
			return
		}

		// 【情境 B：學生/單一科目模式】
		if uid == nil {
			c.HTML(http.StatusOK, "index.html", gin.H{"Logged": false, "AppName": AppName})
			return
		}

		var s models.Student
		// 使用 filterSubject 自動加上 WHERE subject = ...
		result := db.Scopes(filterSubject).First(&s, uid)
		
		if result.Error != nil {
			// 找不到學生資料 -> 可能是新註冊，或是跑錯科目
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

		// 【情境 A：管理員模式登入】
		if IsAdminMode {
			if !isTeacher(gUser.Email) {
				c.String(403, "🚫 抱歉，只有老師可以登入此後台。")
				return
			}
			// 老師登入成功
			session.Set("user_id", "ADMIN_"+gUser.Email) // 特殊 ID 標記
			session.Save()
			c.Redirect(http.StatusSeeOther, "/") // 回到首頁 (Admin Dashboard)
			return
		}

		// 【情境 B：學生模式登入】
		var s models.Student
		// 查詢時加上科目過濾，確保學生是在「當前科目」有註冊
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
		if IsAdminMode { c.Redirect(302, "/"); return } // 後台不需要註冊
		session := sessions.Default(c)
		email := session.Get("temp_email")
		if email == nil { c.Redirect(302, "/"); return }
		c.HTML(200, "register.html", gin.H{"Email": email})
	})

	r.POST("/register", func(c *gin.Context) {
		session := sessions.Default(c)
		email := session.Get("temp_email")
		name := session.Get("temp_name")

		if email == nil { c.Redirect(302, "/"); return }

		var s models.Student
		// 在建立時，一定要寫入當前科目 (CurrentSubject)
		db.Scopes(filterSubject).Where(models.Student{Email: email.(string)}).Attrs(models.Student{
			Name:    name.(string),
			Subject: CurrentSubject, // ★ 關鍵：寫入科目
		}).FirstOrCreate(&s)

		s.StudentID = c.PostForm("student_id")
		s.Course = c.PostForm("course")
		s.Subject = CurrentSubject // 確保更新
		db.Save(&s)

		session.Set("user_id", s.ID)
		session.Delete("temp_email")
		session.Save()
		c.Redirect(302, "/")
	})

	// --- 4. 查詢成績 (學生模式專用) ---
	r.GET("/my-grades", func(c *gin.Context) {
		if IsAdminMode { c.Redirect(302, "/"); return }
		
		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil { c.Redirect(302, "/"); return }

		var s models.Student
		db.Scopes(filterSubject).First(&s, uid)

		// A. 顯示用的詳細成績 (只抓該科目)
		var displayGrades []models.Grade
		db.Scopes(filterSubject).Where("student_id = ? AND item_name != ?", s.StudentID, TotalScoreColName).Order("id asc").Find(&displayGrades)

		// B. 統計用的數據 (需手動 Join 並過濾 Subject 和 Course)
		var myTotalGrade models.Grade
		var classTotals []float64

		// 這裡要很小心：計算排名時，必須鎖定「同科目」且「同班級」
		type Result struct {
			StudentID string
			Score     float64
		}
		var results []Result

		// 1. 先嘗試抓取 Total learning-progress points
		query := db.Table("grades").
			Select("grades.student_id, grades.score").
			Joins("JOIN students ON students.student_id = grades.student_id").
			Where("grades.item_name = ?", TotalScoreColName).
			Where("grades.subject = ?", CurrentSubject).    // ★ 科目過濾
			Where("students.subject = ?", CurrentSubject).  // ★ 科目過濾
			Where("students.course = ?", s.Course)          // ★ 班級過濾

		query.Scan(&results)

		// 如果沒有預先計算好的總分，就自己加總
		if len(results) == 0 {
			// 撈出該班級、該科目的所有成績細項來加總
			type SumResult struct {
				StudentID string
				Total     float64
			}
			db.Table("grades").
				Select("grades.student_id, SUM(grades.score) as total").
				Joins("JOIN students ON students.student_id = grades.student_id").
				Where("grades.subject = ?", CurrentSubject).
				Where("students.subject = ?", CurrentSubject).
				Where("students.course = ?", s.Course).
				Group("grades.student_id").
				Scan(&results) // 這裡結構會自動對應到 Result 的 Score (Total)
		}

		for _, r := range results {
			classTotals = append(classTotals, r.Score)
			if r.StudentID == s.StudentID {
				myTotalGrade.Score = r.Score
			}
		}

		myTotal := myTotalGrade.Score

		// 計算統計數據 (平均、標準差、PR)
		sum := 0.0
		minScore, maxScore := 1000.0, -1.0
		for _, t := range classTotals {
			sum += t
			if t < minScore { minScore = t }
			if t > maxScore { maxScore = t }
		}
		if len(classTotals) == 0 { minScore, maxScore = 0, 0 }

		mean := 0.0
		if len(classTotals) > 0 { mean = sum / float64(len(classTotals)) }

		varianceSum := 0.0
		for _, t := range classTotals { varianceSum += math.Pow(t-mean, 2) }
		stdDev := 0.0
		if len(classTotals) > 0 { stdDev = math.Sqrt(varianceSum / float64(len(classTotals))) }

		sort.Float64s(classTotals)
		rank := 0
		for i, t := range classTotals {
			if t >= myTotal { rank = i; break }
			rank = i + 1
		}
		percentile := 0.0
		if len(classTotals) > 1 {
			percentile = (float64(rank) / float64(len(classTotals))) * 100
		} else if len(classTotals) == 1 { percentile = 100 }

		// Top 3
		var top3 []float64
		count := len(classTotals)
		for i := count - 1; i >= 0 && len(top3) < 3; i-- {
			top3 = append(top3, classTotals[i])
		}
		finalWeight := 100.0 - myTotal
		if finalWeight < 0 { finalWeight = 0 }

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
	// Middleware: 檢查權限
	teacher.Use(func(c *gin.Context) {
		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil {
			c.Redirect(302, "/")
			c.Abort()
			return
		}
		// 如果是 Admin 模式，Session ID 會是 "ADMIN_..."
		isAdminSession := strings.HasPrefix(fmt.Sprintf("%v", uid), "ADMIN_")
		
		// 如果不是 Admin 模式，就檢查資料庫裡的學生是否為老師
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
		// 決定現在要看哪個科目
		targetSubject := CurrentSubject
		
		// 如果是管理員模式，從網址參數讀取科目 (例如 ?subject=circuit)
		if IsAdminMode {
			targetSubject = c.Query("subject")
			if targetSubject == "" {
				c.Redirect(302, "/") // 沒選科目就回首頁選
				return
			}
		}

		var allGrades []models.Grade
		// 查詢該科目的所有成績
		db.Where("subject = ?", targetSubject).Order("created_at desc").Find(&allGrades)

		c.HTML(200, "teacher.html", gin.H{
			"AllGrades": allGrades,
			"Subject":   targetSubject, // 傳給前端，讓上傳表單知道要傳給誰
			"AppName":   AppName,
			"IsAdmin":   IsAdminMode,
		})
	})

	teacher.POST("/upload", func(c *gin.Context) {
		// 決定寫入哪個科目
		targetSubject := CurrentSubject
		if IsAdminMode {
			targetSubject = c.PostForm("subject") // 從 hidden input 讀取
		}

		file, _ := c.FormFile("csv_file")
		f, _ := file.Open()
		defer f.Close()

		reader := csv.NewReader(f)
		reader.FieldsPerRecord = -1
		records, err := reader.ReadAll()
		if err != nil { c.String(400, "CSV 讀取失敗"); return }
		if len(records) < 2 { c.String(400, "無數據"); return }

		header := records[0]
		idIndex := -1
		for i, colName := range header {
			if strings.EqualFold(strings.TrimSpace(colName), "ID") {
				idIndex = i
				break
			}
		}
		if idIndex == -1 { c.String(400, "❌ 找不到 'ID' 欄位"); return }

		ignoreCols := map[string]bool{"No.": true, "Class": true, "ID": true, "Grade": true, "Weight of final exam (%)": true}

		for i, row := range records {
			if i == 0 { continue }
			if len(row) <= idIndex { continue }
			studentID := strings.TrimSpace(row[idIndex])
			if studentID == "" { continue }

			for colIdx, cellValue := range row {
				colName := strings.TrimSpace(header[colIdx])
				if ignoreCols[colName] { continue }

				var score float64
				cellValue = strings.TrimSpace(cellValue)
				if cellValue == "" || strings.EqualFold(cellValue, "NaN") { continue }
				if s, err := strconv.ParseFloat(cellValue, 64); err == nil {
					score = s
				} else { score = 0 }

				// 寫入 DB (包含 Subject)
				db.Clauses(clause.OnConflict{
					// 衝突判斷：學號 + 項目 + 科目 必須唯一
					Columns:   []clause.Column{{Name: "student_id"}, {Name: "item_name"}, {Name: "subject"}},
					DoUpdates: clause.AssignmentColumns([]string{"score", "updated_at"}),
				}).Create(&models.Grade{
					StudentID: studentID,
					ItemName:  colName,
					Score:     score,
					Subject:   targetSubject, // ★ 寫入科目
				})
			}
		}
		
		// 導回 Dashboard (記得帶上 subject 參數)
		redirectUrl := "/teacher/dashboard"
		if IsAdminMode {
			redirectUrl += "?subject=" + targetSubject
		}
		c.Redirect(http.StatusSeeOther, redirectUrl)
	})

	teacher.POST("/delete/:id", func(c *gin.Context) {
		id := c.Param("id")
		// 刪除時，GORM 預設會根據 Primary Key 刪除，所以不太需要擔心刪錯科目
		// 但為了保險，如果是單一模式，可以加 Scope
		db.Scopes(filterSubject).Unscoped().Delete(&models.Grade{}, id)
		
		// 這裡有個小問題：刪除後要導回哪裡？
		// 簡單做法：回到上一頁 (Referer)
		c.Redirect(http.StatusSeeOther, c.Request.Header.Get("Referer"))
	})

	r.Run(":8080")
}