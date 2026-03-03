package controllers

import (
	"encoding/csv"
	"grade-system/initializers"
	"grade-system/models"
	"grade-system/utils"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm/clause"
)

// TeacherDashboard 顯示管理介面
func TeacherDashboard(c *gin.Context) {
	targetSubject := initializers.CurrentSubject
	if initializers.IsAdminMode {
		targetSubject = c.Query("subject")
		if targetSubject == "" {
			c.Redirect(302, "/")
			return
		}
	}

	var allGrades []models.Grade
	initializers.DB.Where("subject = ?", targetSubject).Order("created_at desc").Find(&allGrades)

	type RosterRow struct {
		Class     string
		StudentID string
		Name      string
		Email     string
	}
	var rosterRows []RosterRow

	initializers.DB.Table("rosters").
		Select("rosters.class, rosters.student_id, rosters.name, students.email").
		Joins("LEFT JOIN students ON students.student_id = rosters.student_id").
		Where("rosters.subject = ?", targetSubject).
		Order("rosters.class ASC, rosters.student_id ASC").
		Scan(&rosterRows)

	c.HTML(200, "teacher.html", gin.H{
		"AllGrades":  allGrades,
		"RosterList": rosterRows,
		"Subject":    targetSubject,
		"AppName":    initializers.AppName,
		"IsAdmin":    initializers.IsAdminMode,
	})
}

// --- 批次匯入功能 (CSV) ---

func UploadGrades(c *gin.Context) {
	targetSubject := initializers.CurrentSubject
	if initializers.IsAdminMode {
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

	var validStudentIDs []string
	initializers.DB.Model(&models.Roster{}).Where("subject = ?", targetSubject).Pluck("student_id", &validStudentIDs)
	validStudentMap := make(map[string]bool)
	for _, id := range validStudentIDs {
		validStudentMap[utils.CleanID(id)] = true
	}

	header := records[0]
	idIndex := -1
	for i, colName := range header {
		cleanName := strings.ToLower(utils.CleanHeader(colName))
		if cleanName == "id" || cleanName == "student id" || cleanName == "student_id" || cleanName == "學號" {
			idIndex = i
			break
		}
	}

	if idIndex == -1 {
		c.String(400, "❌ 找不到 ID 欄位")
		return
	}

	successCount, skippedCount := 0, 0
	ignoreCols := map[string]bool{"No.": true, "No": true, "class": true, "id": true, "grade": true}

	for i, row := range records {
		if i == 0 || len(row) <= idIndex { continue }
		studentID := utils.CleanID(row[idIndex])
		if studentID == "" || !validStudentMap[studentID] {
			skippedCount++
			continue
		}

		for colIdx, cellValue := range row {
			colName := utils.CleanHeader(header[colIdx])
			if ignoreCols[colName] || ignoreCols[strings.ToLower(colName)] { continue }

			score, _ := strconv.ParseFloat(strings.TrimSpace(cellValue), 64)
			initializers.DB.Clauses(clause.OnConflict{
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
	log.Printf("匯入完成：寫入 %d 筆，略過 %d 筆。", successCount, skippedCount)
	redirectBack(c, targetSubject)
}

func UploadRoster(c *gin.Context) {
	targetSubject := initializers.CurrentSubject
	if initializers.IsAdminMode {
		targetSubject = c.PostForm("subject")
	}

	file, _ := c.FormFile("roster_file")
	f, _ := file.Open()
	defer f.Close()

	reader := csv.NewReader(f)
	records, _ := reader.ReadAll()

	classIndex, idIndex := 1, 2 // 預設值
	for i, col := range records[0] {
		cName := strings.ToLower(utils.CleanHeader(col))
		if cName == "class" || cName == "班級" { classIndex = i }
		if cName == "id" || cName == "學號" { idIndex = i }
	}

	for i, row := range records {
		if i == 0 || len(row) <= idIndex { continue }
		sid := utils.CleanID(row[idIndex])
		class := strings.TrimSpace(row[classIndex])
		if sid == "" { continue }

		initializers.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "student_id"}, {Name: "subject"}},
			DoUpdates: clause.AssignmentColumns([]string{"class", "updated_at"}),
		}).Create(&models.Roster{StudentID: sid, Class: class, Subject: targetSubject})
	}
	redirectBack(c, targetSubject)
}

// --- 手動單筆管理功能 (CRUD) ---

func PostRoster(c *gin.Context) {
	targetSubject := getTargetSubject(c)
	sid := utils.CleanID(c.PostForm("student_id"))
	class := strings.TrimSpace(c.PostForm("class"))

	if sid != "" {
		initializers.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "student_id"}, {Name: "subject"}},
			DoUpdates: clause.AssignmentColumns([]string{"class", "updated_at"}),
		}).Create(&models.Roster{StudentID: sid, Class: class, Subject: targetSubject})
	}
	redirectBack(c, targetSubject)
}

func DeleteRoster(c *gin.Context) {
	targetSubject := initializers.CurrentSubject
	sid := c.Query("student_id")
	initializers.DB.Where("student_id = ? AND subject = ?", sid, targetSubject).Delete(&models.Roster{})
	initializers.DB.Where("student_id = ? AND subject = ?", sid, targetSubject).Delete(&models.Grade{})
	redirectBack(c, targetSubject)
}

func PostGrade(c *gin.Context) {
	targetSubject := getTargetSubject(c)
	sid := utils.CleanID(c.PostForm("student_id"))
	itemName := strings.TrimSpace(c.PostForm("item_name"))
	score, _ := strconv.ParseFloat(c.PostForm("score"), 64)

	if sid != "" && itemName != "" {
		initializers.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "student_id"}, {Name: "item_name"}, {Name: "subject"}},
			DoUpdates: clause.AssignmentColumns([]string{"score", "updated_at"}),
		}).Create(&models.Grade{StudentID: sid, ItemName: itemName, Score: score, Subject: targetSubject})
	}
	redirectBack(c, targetSubject)
}

func DeleteGrade(c *gin.Context) {
	targetSubject := initializers.CurrentSubject
	sid := c.Query("student_id")
	item := c.Query("item_name")
	initializers.DB.Where("student_id = ? AND item_name = ? AND subject = ?", sid, item, targetSubject).Delete(&models.Grade{})
	redirectBack(c, targetSubject)
}

// --- 批次清空功能 (危險區) ---

// --- 修正：原有的清空功能 (改名以防混淆) ---
func ClearRoster(c *gin.Context) {
	targetSubject := initializers.CurrentSubject
	if initializers.IsAdminMode {
		targetSubject = c.PostForm("subject")
	}

	log.Printf("⚠️ 正在 [清空科目] %s 的所有名單...", targetSubject)
	initializers.DB.Unscoped().Where("subject = ?", targetSubject).Delete(&models.Roster{})
	
	redirectBack(c, targetSubject)
}

func ClearAllGrades(c *gin.Context) {
	targetSubject := getTargetSubject(c)
	initializers.DB.Unscoped().Where("subject = ?", targetSubject).Delete(&models.Grade{})
	redirectBack(c, targetSubject)
}

// --- 內部輔助函式 ---

func getTargetSubject(c *gin.Context) string {
	if initializers.IsAdminMode {
		return c.PostForm("subject")
	}
	return initializers.CurrentSubject
}

func redirectBack(c *gin.Context, subject string) {
	path := "/teacher/dashboard"
	if initializers.IsAdminMode {
		path += "?subject=" + subject
	}
	c.Redirect(http.StatusSeeOther, path)
}

// UnbindStudentEmail 移除學生的 Email 綁定 (不影響成績與名單)
func UnbindStudentEmail(c *gin.Context) {
	targetSubject := initializers.CurrentSubject
	if initializers.IsAdminMode {
		targetSubject = c.Query("subject")
	}
	
	sid := c.Query("student_id")

	// 僅從 students 資料表刪除紀錄，這會解除 Email 與學號的連結
	// 使用 Unscoped() 是因為模型中使用了 gorm.Model，包含 DeletedAt 軟刪除
	err := initializers.DB.Unscoped().
		Where("student_id = ? AND subject = ?", sid, targetSubject).
		Delete(&models.Student{}).Error

	if err != nil {
		log.Printf("移除綁定失敗: %v", err)
	}

	redirectBack(c, targetSubject)
}

// --- 修正：刪除單一學生 (名單與成績連帶刪除) ---
func DeleteSingleRoster(c *gin.Context) {
	targetSubject := initializers.CurrentSubject
	if initializers.IsAdminMode {
		targetSubject = c.Query("subject")
	}
	
	sid := c.Query("student_id") // 從網址取得學號

	if sid == "" {
		c.String(400, "無效的學號")
		return
	}

	// 🌟 核心修正：必須加上 student_id 條件！
	initializers.DB.Unscoped().Where("student_id = ? AND subject = ?", sid, targetSubject).Delete(&models.Roster{})
	initializers.DB.Unscoped().Where("student_id = ? AND subject = ?", sid, targetSubject).Delete(&models.Grade{})
	redirectBack(c, targetSubject)
}