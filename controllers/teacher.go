package controllers

import (
	"encoding/csv"
	"fmt"
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
		"AllGrades":   allGrades,
		"RosterList":  rosterRows,
		"Subject":     targetSubject,
		"AppName":     initializers.AppName,
		"IsAdmin":     initializers.IsAdminMode,
	})
}

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
	if len(records) < 2 {
		c.String(400, "無數據")
		return
	}

	var validStudentIDs []string
	initializers.DB.Model(&models.Roster{}).Where("subject = ?", targetSubject).Pluck("student_id", &validStudentIDs)
	validStudentMap := make(map[string]bool)
	for _, id := range validStudentIDs {
		validStudentMap[id] = true
	}

	header := records[0]
	if len(header) > 0 {
		header[0] = strings.TrimPrefix(header[0], "\ufeff")
	}

	idIndex := -1
	for i, colName := range header {
		cleanName := strings.ToLower(utils.CleanHeader(colName))
		if cleanName == "id" || cleanName == "student id" || cleanName == "student_id" || cleanName == "學號" {
			idIndex = i
			break
		}
	}

	if idIndex == -1 {
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

		if !validStudentMap[studentID] {
			skippedCount++
			continue
		}

		for colIdx, cellValue := range row {
			colName := utils.CleanHeader(header[colIdx])
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

	log.Printf("匯入完成。寫入 %d 筆，略過 %d 筆。", successCount, skippedCount)

	redirectUrl := "/teacher/dashboard"
	if initializers.IsAdminMode {
		redirectUrl += "?subject=" + targetSubject
	}
	c.Redirect(http.StatusSeeOther, redirectUrl)
}

func UploadRoster(c *gin.Context) {
	targetSubject := initializers.CurrentSubject
	if initializers.IsAdminMode {
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

	classIndex := 1
	idIndex := 2

	header := records[0]
	if len(header) > 0 {
		header[0] = strings.TrimPrefix(header[0], "\ufeff")
		for i, col := range header {
			cName := strings.ToLower(utils.CleanHeader(col))
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

		initializers.DB.Clauses(clause.OnConflict{
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
	if initializers.IsAdminMode {
		redirectUrl += "?subject=" + targetSubject
	}
	c.Redirect(http.StatusSeeOther, redirectUrl)
}

func DeleteRoster(c *gin.Context) {
	targetSubject := initializers.CurrentSubject
	if initializers.IsAdminMode {
		targetSubject = c.PostForm("subject")
	}

	log.Printf("🗑️ 正在 [物理清空] %s 的修課名單...", targetSubject)
	if err := initializers.DB.Unscoped().Where("subject = ?", targetSubject).Delete(&models.Roster{}).Error; err != nil {
		c.String(500, "刪除失敗")
		return
	}

	redirectUrl := "/teacher/dashboard"
	if initializers.IsAdminMode {
		redirectUrl += "?subject=" + targetSubject
	}
	c.Redirect(http.StatusSeeOther, redirectUrl)
}

func DeleteAllGrades(c *gin.Context) {
	targetSubject := initializers.CurrentSubject
	if initializers.IsAdminMode {
		targetSubject = c.PostForm("subject")
	}

	log.Printf("🗑️ 正在 [物理清空] %s 的所有成績...", targetSubject)
	if err := initializers.DB.Unscoped().Where("subject = ?", targetSubject).Delete(&models.Grade{}).Error; err != nil {
		c.String(500, "刪除失敗")
		return
	}

	redirectUrl := "/teacher/dashboard"
	if initializers.IsAdminMode {
		redirectUrl += "?subject=" + targetSubject
	}
	c.Redirect(http.StatusSeeOther, redirectUrl)
}

func DeleteGrade(c *gin.Context) {
	id := c.Param("id")
	initializers.DB.Scopes(utils.FilterSubject).Unscoped().Delete(&models.Grade{}, id)
	c.Redirect(http.StatusSeeOther, c.Request.Header.Get("Referer"))
}