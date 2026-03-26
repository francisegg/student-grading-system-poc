package controllers

import (
	"encoding/csv"
	"grade-system/initializers"
	"grade-system/models"
	"grade-system/utils"
	// "log"
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

	// 🌟 修正：確保 Join 的時候有比對 subject，且排除幽靈紀錄
	initializers.DB.Table("rosters").
		Select("rosters.class, rosters.student_id, rosters.name, students.email").
		Joins("LEFT JOIN students ON students.student_id = rosters.student_id AND students.subject = rosters.subject AND students.deleted_at IS NULL").
		Where("rosters.subject = ?", targetSubject).
		Where("rosters.deleted_at IS NULL").
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

// UploadGrades 處理成績 CSV
func UploadGrades(c *gin.Context) {
	targetSubject := initializers.CurrentSubject
	if initializers.IsAdminMode {
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

	// 🌟 修正：忽略欄位加入 "name" 和 "姓名"，防止變成成績項目！
	ignoreCols := map[string]bool{"no.": true, "no": true, "class": true, "id": true, "grade": true, "name": true, "姓名": true}

	for i, row := range records {
		if i == 0 || len(row) <= idIndex { continue }
		studentID := utils.CleanID(row[idIndex])
		if studentID == "" || !validStudentMap[studentID] { continue }

		for colIdx, cellValue := range row {
			colName := utils.CleanHeader(header[colIdx])
			if ignoreCols[strings.ToLower(colName)] { continue }

			score, _ := strconv.ParseFloat(strings.TrimSpace(cellValue), 64)
			// 加入 deleted_at 確保幽靈紀錄可以在這一步復活
			initializers.DB.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "student_id"}, {Name: "item_name"}, {Name: "subject"}},
				DoUpdates: clause.AssignmentColumns([]string{"score", "updated_at", "deleted_at"}),
			}).Create(&models.Grade{
				StudentID: studentID,
				ItemName:  colName,
				Score:     score,
				Subject:   targetSubject,
			})
		}
	}
	redirectBack(c, targetSubject)
}

// UploadRoster 處理名單 CSV
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

	classIndex, idIndex, nameIndex := -1, -1, -1
	for i, col := range records[0] {
		cName := strings.ToLower(utils.CleanHeader(col))
		if cName == "class" || cName == "班級" { classIndex = i }
		if cName == "id" || cName == "學號" { idIndex = i }
		if cName == "name" || cName == "姓名" { nameIndex = i }
	}

	for i, row := range records {
		if i == 0 || len(row) <= idIndex { continue }
		sid := utils.CleanID(row[idIndex])
		if sid == "" { continue }
		
		class := ""
		if classIndex != -1 && len(row) > classIndex {
			class = strings.TrimSpace(row[classIndex])
		}
		
		name := ""
		if nameIndex != -1 && len(row) > nameIndex {
			name = strings.TrimSpace(row[nameIndex])
		}

		initializers.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "student_id"}, {Name: "subject"}},
			DoUpdates: clause.AssignmentColumns([]string{"class", "name", "updated_at", "deleted_at"}),
		}).Create(&models.Roster{StudentID: sid, Class: class, Name: name, Subject: targetSubject})
	}
	redirectBack(c, targetSubject)
}

// --- 手動管理與解綁 ---

func PostRoster(c *gin.Context) {
	targetSubject := getTargetSubject(c)
	sid := utils.CleanID(c.PostForm("student_id"))
	class := strings.TrimSpace(c.PostForm("class"))

	if sid != "" {
		initializers.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "student_id"}, {Name: "subject"}},
			DoUpdates: clause.AssignmentColumns([]string{"class", "updated_at", "deleted_at"}),
		}).Create(&models.Roster{StudentID: sid, Class: class, Subject: targetSubject})
	}
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
			DoUpdates: clause.AssignmentColumns([]string{"score", "updated_at", "deleted_at"}),
		}).Create(&models.Grade{StudentID: sid, ItemName: itemName, Score: score, Subject: targetSubject})
	}
	redirectBack(c, targetSubject)
}

func DeleteGrade(c *gin.Context) {
	targetSubject := initializers.CurrentSubject
	sid := c.Query("student_id")
	item := c.Query("item_name")
	// 這裡保留普通的 Delete() 讓他變成軟刪除
	initializers.DB.Where("student_id = ? AND item_name = ? AND subject = ?", sid, item, targetSubject).Delete(&models.Grade{})
	redirectBack(c, targetSubject)
}

// 🌟 修正：精準的刪除單一學生名單與成績
func DeleteSingleRoster(c *gin.Context) {
	targetSubject := initializers.CurrentSubject
	if initializers.IsAdminMode {
		targetSubject = c.Query("subject")
	}
	
	sid := c.Query("student_id")
	if sid != "" {
		// 使用 Unscoped() 進行硬刪除，避免產生幽靈紀錄
		initializers.DB.Unscoped().Where("student_id = ? AND subject = ?", sid, targetSubject).Delete(&models.Roster{})
		initializers.DB.Unscoped().Where("student_id = ? AND subject = ?", sid, targetSubject).Delete(&models.Grade{})
	}
	redirectBack(c, targetSubject)
}

// 解除綁定 Email
func UnbindStudentEmail(c *gin.Context) {
	targetSubject := initializers.CurrentSubject
	if initializers.IsAdminMode {
		targetSubject = c.Query("subject")
	}
	
	sid := c.Query("student_id")
	// 硬刪除，讓學生可以重新綁定
	initializers.DB.Unscoped().Where("student_id = ? AND subject = ?", sid, targetSubject).Delete(&models.Student{})
	redirectBack(c, targetSubject)
}

// --- 危險區：全部清空 ---

func ClearRoster(c *gin.Context) {
	targetSubject := getTargetSubject(c)
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