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
)

// 檢查是否為老師
func isTeacher(email string) bool {
	whitelist := os.Getenv("TEACHER_WHITELIST")
	return strings.Contains(whitelist, email)
}

func init() {
	if err := godotenv.Load(); err != nil {
		log.Println("找不到 .env 檔案，使用系統變數")
	}

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Taipei",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
	var err error
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("資料庫連線失敗: ", err)
	}
	db.AutoMigrate(&models.Student{}, &models.Grade{})

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
		c.HTML(http.StatusOK, "index.html", gin.H{"Logged": true, "User": s, "IsTeacher": isTeacher(s.Email)})
	})

	// --- 2. 登入/登出 ---
	r.GET("/login", func(c *gin.Context) {
		url := googleOauthConfig.AuthCodeURL("state")
		c.Redirect(http.StatusTemporaryRedirect, url)
	})

	r.GET("/auth/callback", func(c *gin.Context) {
		token, err := googleOauthConfig.Exchange(context.Background(), c.Query("code"))
		if err != nil { c.Redirect(302, "/"); return }
		
		resp, _ := http.Get("https://www.googleapis.com/oauth2/v2/userinfo?access_token=" + token.AccessToken)
		defer resp.Body.Close()
		data, _ := ioutil.ReadAll(resp.Body)

		var gUser struct{ Email, Name string }
		json.Unmarshal(data, &gUser)

		var s models.Student
		result := db.Where("email = ?", gUser.Email).First(&s)
		session := sessions.Default(c)
		
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
		session := sessions.Default(c)
		email := session.Get("temp_email")
		if email == nil { c.Redirect(302, "/"); return }
		c.HTML(200, "register.html", gin.H{"Email": email})
	})

	r.POST("/register", func(c *gin.Context) {
		session := sessions.Default(c)
		email := session.Get("temp_email")
		name := session.Get("temp_name")
		
		var s models.Student
		db.Where(models.Student{Email: email.(string)}).Attrs(models.Student{Name: name.(string)}).FirstOrCreate(&s)
		s.StudentID = c.PostForm("student_id")
		s.Course = c.PostForm("course")
		db.Save(&s)

		session.Set("user_id", s.ID)
		session.Delete("temp_email")
		session.Save()
		c.Redirect(302, "/")
	})

	// --- 4. 學生看成績 ---
	r.GET("/my-grades", func(c *gin.Context) {
		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil { c.Redirect(302, "/"); return }

		var s models.Student
		db.First(&s, uid)

		var grades []models.Grade
		// 依照 ID 排序，確保圖表時間軸正確
		db.Where("student_id = ?", s.StudentID).Order("id asc").Find(&grades)
		c.HTML(200, "my_grades.html", gin.H{"User": s, "Grades": grades})
	})

	// --- 5. 老師功能 ---
	teacher := r.Group("/teacher")
	teacher.Use(func(c *gin.Context) {
		session := sessions.Default(c)
		uid := session.Get("user_id")
		var s models.Student
		if uid == nil || db.First(&s, uid).Error != nil || !isTeacher(s.Email) {
			c.String(403, "🚫 權限不足")
			c.Abort()
			return
		}
		c.Next()
	})

	teacher.GET("/dashboard", func(c *gin.Context) {
		var allGrades []models.Grade
		db.Order("created_at desc").Find(&allGrades)
		c.HTML(200, "teacher.html", gin.H{"AllGrades": allGrades})
	})

	// --- 升級版上傳功能 (支援動態欄位 & UTF-8) ---
	teacher.POST("/upload", func(c *gin.Context) {
		file, _ := c.FormFile("csv_file")
		f, _ := file.Open()
		defer f.Close()

		// 直接使用 CSV Reader (Go 預設支援 UTF-8)
		reader := csv.NewReader(f)
		reader.FieldsPerRecord = -1 // 允許欄位長度不一致
		records, err := reader.ReadAll()
		if err != nil {
			c.String(400, "CSV 讀取失敗: "+err.Error())
			return
		}

		if len(records) < 2 {
			c.String(400, "CSV 內容為空或無數據")
			return
		}

		// 1. 解析標題列，找出 "ID" 在第幾欄
		header := records[0]
		idIndex := -1
		for i, colName := range header {
			// 去除空格並忽略大小寫比較
			if strings.EqualFold(strings.TrimSpace(colName), "ID") {
				idIndex = i
				break
			}
		}

		if idIndex == -1 {
			c.String(400, "❌ 找不到 'ID' 欄位，請檢查 CSV 標題")
			return
		}

		// 定義要忽略的非成績欄位 (Metadata)
		ignoreCols := map[string]bool{
			"No.": true, "Class": true, "ID": true, "Grade": true,
			"Total learning-progress points": true, "Weight of final exam (%)": true,
		}

		count := 0
		// 2. 遍歷每一列數據
		for i, row := range records {
			if i == 0 { continue } // 跳過標題

			// 取得學號
			if len(row) <= idIndex { continue }
			studentID := strings.TrimSpace(row[idIndex])
			if studentID == "" { continue }

			// 3. 遍歷該列的所有欄位 (把每個欄位都當作一個成績項目)
			for colIdx, cellValue := range row {
				colName := strings.TrimSpace(header[colIdx])

				// 如果是基本資料欄位，就跳過
				if ignoreCols[colName] {
					continue
				}

				// 處理分數 (處理 "缺考", "NaN", 空白)
				var score float64
				cellValue = strings.TrimSpace(cellValue)
				if cellValue == "" || strings.EqualFold(cellValue, "NaN") {
					continue // 空值不匯入
				}
				
				// 嘗試將文字轉為數字，失敗則預設為 0 (例如 '缺考')
				if s, err := strconv.ParseFloat(cellValue, 64); err == nil {
					score = s
				} else {
					score = 0
				}

				// 寫入資料庫
				db.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "student_id"}, {Name: "item_name"}},
					DoUpdates: clause.AssignmentColumns([]string{"score", "updated_at"}),
				}).Create(&models.Grade{
					StudentID: studentID,
					ItemName:  colName, // 使用標題作為項目名稱 (如 "Midterm", "9/15")
					Score:     score,
				})
				count++
			}
		}

		c.Redirect(http.StatusSeeOther, "/teacher/dashboard")
	})

	teacher.POST("/delete/:id", func(c *gin.Context) {
		id := c.Param("id")
		db.Unscoped().Delete(&models.Grade{}, id)
		c.Redirect(http.StatusSeeOther, "/teacher/dashboard")
	})

	r.Run(":8080")
}