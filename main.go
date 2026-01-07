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
)

const TotalScoreColName = "Total learning-progress points"

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
		RedirectURL:  "http://www.teaegg.space/auth/callback",
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

	// --- 4. 學生看成績 (加入最大最小值計算) ---
	r.GET("/my-grades", func(c *gin.Context) {
		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil { c.Redirect(302, "/"); return }

		var s models.Student
		db.First(&s, uid)

		// A. 顯示用的詳細成績
		var displayGrades []models.Grade
		db.Where("student_id = ? AND item_name != ?", s.StudentID, TotalScoreColName).Order("id asc").Find(&displayGrades)

		// B. 統計用的數據
		var myTotalGrade models.Grade
		var classTotals []float64

		// 先找有沒有 "Total learning-progress points"
		var totalRows []models.Grade
		db.Where("item_name = ?", TotalScoreColName).Find(&totalRows)

		if len(totalRows) > 0 {
			for _, r := range totalRows {
				classTotals = append(classTotals, r.Score)
				if r.StudentID == s.StudentID {
					myTotalGrade = r
				}
			}
		} else {
			// Fallback: 自己加總
			var allGrades []models.Grade
			db.Find(&allGrades)
			studentMap := make(map[string]float64)
			for _, g := range allGrades {
				studentMap[g.StudentID] += g.Score
			}
			for sid, score := range studentMap {
				classTotals = append(classTotals, score)
				if sid == s.StudentID {
					myTotalGrade.Score = score
				}
			}
		}

		myTotal := myTotalGrade.Score
		
		// 計算基本統計
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

		// 計算 PR
		sort.Float64s(classTotals) // 這一步排序很重要 (由小到大)
		rank := 0
		for i, t := range classTotals {
			if t >= myTotal { rank = i; break }
			rank = i + 1
		}
		percentile := 0.0
		if len(classTotals) > 1 {
			percentile = (float64(rank) / float64(len(classTotals))) * 100
		} else if len(classTotals) == 1 { percentile = 100 }

		// --- 新增功能區 Start ---
		
		// 1. 取出前三名 (classTotals 已經是由小到大排序)
		var top3 []float64
		count := len(classTotals)
		// 從後面 (最大值) 開始抓
		for i := count - 1; i >= 0 && len(top3) < 3; i-- {
			top3 = append(top3, classTotals[i])
		}

		// 2. 計算期末佔比 (100 - 目前總分)
		finalWeight := 100.0 - myTotal
		if finalWeight < 0 { finalWeight = 0 } // 防止超過100變負數

		// --- 新增功能區 End ---

		c.HTML(200, "my_grades.html", gin.H{
			"User":        s,
			"Grades":      displayGrades,
			"MyTotal":     myTotal,
			"ClassMean":   mean,
			"ClassStdDev": stdDev,
			"ClassMin":    minScore,
			"ClassMax":    maxScore,
			"Percentile":  int(percentile),
			"Top3":        top3,        // 傳遞前三名
			"FinalWeight": finalWeight, // 傳遞期末佔比
		})
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

	teacher.POST("/upload", func(c *gin.Context) {
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

		// 忽略欄位清單 (Total learning-progress points 需允許寫入)
		ignoreCols := map[string]bool{
			"No.": true, "Class": true, "ID": true, "Grade": true,
			"Weight of final exam (%)": true,
		}

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

				db.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "student_id"}, {Name: "item_name"}},
					DoUpdates: clause.AssignmentColumns([]string{"score", "updated_at"}),
				}).Create(&models.Grade{
					StudentID: studentID,
					ItemName:  colName,
					Score:     score,
				})
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