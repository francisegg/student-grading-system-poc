package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"grade-system/models" // 注意：這裡要跟你的 go.mod 名字一樣

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

var (
	db                *gorm.DB
	googleOauthConfig *oauth2.Config
)

// 檢查是否為老師 (白名單機制)
func isTeacher(email string) bool {
	whitelist := os.Getenv("TEACHER_WHITELIST")
	return strings.Contains(whitelist, email)
}

func init() {
	// 1. 載入 .env
	if err := godotenv.Load(); err != nil {
		log.Println("找不到 .env 檔案，使用系統變數")
	}

	// 2. 連線資料庫
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Taipei",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
	var err error
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("資料庫連線失敗: ", err)
	}
	// 自動建立資料表
	db.AutoMigrate(&models.Student{}, &models.Grade{})

	// 3. Google 設定
	googleOauthConfig = &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  "http://localhost:8080/auth/callback",
		Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile"},
		Endpoint:     google.Endpoint,
	}
}

func main() {
	r := gin.Default()
	// 設定 Session 密鑰
	store := cookie.NewStore([]byte(os.Getenv("SESSION_SECRET")))
	r.Use(sessions.Sessions("mysession", store))
	r.LoadHTMLGlob("templates/*")

	// --- 1. 首頁 ---
	r.GET("/", func(c *gin.Context) {
		session := sessions.Default(c)
		uid := session.Get("user_id")
		
		if uid == nil {
			c.HTML(http.StatusOK, "index.html", gin.H{"Logged": false})
			return
		}

		var s models.Student
		db.First(&s, uid)
		c.HTML(http.StatusOK, "index.html", gin.H{
			"Logged":    true,
			"User":      s,
			"IsTeacher": isTeacher(s.Email),
		})
	})

	// --- 2. Google 登入流程 ---
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

		// 檢查資料庫
		var s models.Student
		result := db.Where("email = ?", gUser.Email).First(&s)

		session := sessions.Default(c)
		
		// 如果是新用戶，或者還沒綁定學號 -> 去註冊頁
		if result.Error == gorm.ErrRecordNotFound || s.StudentID == "" {
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

	// --- 3. 註冊綁定 ---
	r.GET("/register", func(c *gin.Context) {
		session := sessions.Default(c)
		email := session.Get("temp_email")
		if email == nil { c.Redirect(302, "/"); return }
		c.HTML(200, "register.html", gin.H{"Email": email})
	})

	r.POST("/register", func(c *gin.Context) {
		session := sessions.Default(c)
		email := session.Get("temp_email")
		name := session.Get("temp_name")
		
		// 寫入資料庫
		var s models.Student
		db.Where(models.Student{Email: email.(string)}).Attrs(models.Student{Name: name.(string)}).FirstOrCreate(&s)
		
		s.StudentID = c.PostForm("student_id")
		s.Course = c.PostForm("course")
		db.Save(&s)

		// 更新 Session
		session.Set("user_id", s.ID)
		session.Delete("temp_email")
		session.Save()
		c.Redirect(302, "/")
	})

	// --- 4. 學生查看成績 ---
	r.GET("/my-grades", func(c *gin.Context) {
		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil { c.Redirect(302, "/"); return }

		var s models.Student
		db.First(&s, uid)

		var grades []models.Grade
		db.Where("student_id = ?", s.StudentID).Find(&grades)

		c.HTML(200, "my_grades.html", gin.H{"User": s, "Grades": grades})
	})

	// --- 5. 老師後台 (需驗證權限) ---
	teacher := r.Group("/teacher")
	teacher.Use(func(c *gin.Context) {
		session := sessions.Default(c)
		uid := session.Get("user_id")
		var s models.Student
		// 檢查是否登入 & 是否在白名單
		if uid == nil || db.First(&s, uid).Error != nil || !isTeacher(s.Email) {
			c.String(403, "🚫 權限不足：您不是授權的老師")
			c.Abort()
			return
		}
		c.Next()
	})

	teacher.GET("/dashboard", func(c *gin.Context) {
		// 1. 撈出所有成績
		var allGrades []models.Grade
		// 這裡使用 Preload 或是簡單查詢，我們先用簡單查詢並按時間排序
		db.Order("created_at desc").Find(&allGrades)

		// 2. 傳給 HTML
		c.HTML(200, "teacher.html", gin.H{
			"AllGrades": allGrades,
		})
	})

	teacher.POST("/upload", func(c *gin.Context) {
		file, _ := c.FormFile("csv_file")
		f, _ := file.Open()
		defer f.Close() // 養成好習慣，記得關檔

		// --- 關鍵修正開始 ---
		// 建立一個轉換器：將 Big5 (Windows Excel 預設) 轉為 UTF-8
		// 這樣資料庫才看得懂中文
		utf8Reader := transform.NewReader(f, traditionalchinese.Big5.NewDecoder())
		
		// 使用轉換過後的 reader 來讀取 CSV
		r := csv.NewReader(utf8Reader)
		// 允許欄位數量變動 (避免因為 Excel 多餘空格導致報錯)
		r.FieldsPerRecord = -1 
		records, err := r.ReadAll()
		// --- 關鍵修正結束 ---

		if err != nil {
			c.String(400, "CSV 讀取失敗，請確認格式: "+err.Error())
			return
		}
		
		successCount := 0
		for i, row := range records {
			if i == 0 { continue } // 跳過標題
			if len(row) < 3 { continue }
			
			// 解析分數 (處理可能的空白)
			var score float64
			_, err := fmt.Sscanf(strings.TrimSpace(row[1]), "%f", &score)
			if err != nil { continue } // 分數格式不對就跳過
			
			// 建構資料物件
			grade := models.Grade{
				StudentID: strings.TrimSpace(row[0]),
				Score:     score,
				ItemName:  strings.TrimSpace(row[2]),
			}

			// Upsert: 衝突時更新
			db.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "student_id"}, {Name: "item_name"}},
				DoUpdates: clause.AssignmentColumns([]string{"score", "updated_at"}),
			}).Create(&grade)
			
			successCount++
		}

		

		// 重新導向回儀表板
		c.Redirect(http.StatusSeeOther, "/teacher/dashboard")
	})
	
	// --- 新增：刪除成績路由 ---
	teacher.POST("/delete/:id", func(c *gin.Context) {
		id := c.Param("id")
		
		// 使用 GORM 的 Delete 方法，根據主鍵 ID 刪除
		// Unscoped() 代表真的從資料庫移除 (Hard Delete)
		// 如果不加 Unscoped()，預設是軟刪除 (Soft Delete，只標記 deleted_at 時間)
		if err := db.Unscoped().Delete(&models.Grade{}, id).Error; err != nil {
			c.String(500, "刪除失敗: "+err.Error())
			return
		}

		// 刪除完成後，跳轉回儀表板
		c.Redirect(http.StatusSeeOther, "/teacher/dashboard")
	})

	r.Run(":8080")
}