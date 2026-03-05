package controllers

import (
	"fmt"
	"grade-system/initializers"
	"grade-system/models"
	"grade-system/utils"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// 定義要排除的非成績欄位 (黑名單)
var IgnoredGradeItems = []string{
	"Total learning-progress points",
	"No.", "No", "NO",
	"Class", "class",
	"ID", "id", "Student ID", "student_id",
	"Name", "name", "姓名",
	"Weight of final exam (%)",
}

func ShowIndex(c *gin.Context) {
	session := sessions.Default(c)
	uid := session.Get("user_id")

	if initializers.IsAdminMode {
		if uid == nil {
			c.HTML(http.StatusOK, "index.html", gin.H{"Logged": false, "AppName": initializers.AppName, "IsAdminMode": true})
			return
		}
		var subjects []string
		initializers.DB.Model(&models.Grade{}).Distinct("subject").Pluck("subject", &subjects)

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
			"AppName":   initializers.AppName,
			"UserEmail": userEmail,
		})
		return
	}

	if uid == nil {
		c.HTML(http.StatusOK, "index.html", gin.H{"Logged": false, "AppName": initializers.AppName})
		return
	}

	var s models.Student
	if err := initializers.DB.Scopes(utils.FilterSubject).First(&s, uid).Error; err != nil {
		c.Redirect(http.StatusSeeOther, "/logout")
		return
	}

	c.HTML(http.StatusOK, "index.html", gin.H{
		"Logged":    true,
		"User":      s,
		"IsTeacher": utils.IsTeacher(s.Email),
		"AppName":   initializers.AppName,
	})
}

func ShowRegister(c *gin.Context) {
	if initializers.IsAdminMode {
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
}

func Register(c *gin.Context) {
	session := sessions.Default(c)
	email := session.Get("temp_email")
	if email == nil {
		c.Redirect(302, "/")
		return
	}
	userEmail := email.(string)
	userName := ""
	if googleName := session.Get("temp_name"); googleName != nil {
		userName = googleName.(string)
	}

	inputID := strings.TrimSpace(c.PostForm("student_id"))

	var roster models.Roster
	if err := initializers.DB.Where("student_id = ? AND subject = ?", inputID, initializers.CurrentSubject).First(&roster).Error; err != nil {
		c.String(400, "❌ 驗證失敗：此學號不在名單中，請檢查輸入。")
		return
	}

	var existStudent models.Student
	if err := initializers.DB.Scopes(utils.FilterSubject).Where("student_id = ?", inputID).First(&existStudent).Error; err == nil {
		c.String(400, "❌ 綁定失敗：此學號已經被註冊過了！")
		return
	}

	newStudent := models.Student{
		Email:     userEmail,
		Name:      userName,
		StudentID: roster.StudentID,
		Class:     roster.Class,
		Subject:   initializers.CurrentSubject,
	}

	if err := initializers.DB.Create(&newStudent).Error; err != nil {
		c.String(500, "資料庫寫入失敗")
		return
	}

	session.Set("user_id", newStudent.ID)
	session.Delete("temp_email")
	session.Delete("temp_name")
	session.Save()
	c.Redirect(302, "/")
}

func ShowMyGrades(c *gin.Context) {
	if initializers.IsAdminMode {
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
	initializers.DB.Scopes(utils.FilterSubject).First(&s, uid)

	var globalGradeCount int64
	initializers.DB.Model(&models.Grade{}).Where("subject = ?", initializers.CurrentSubject).Count(&globalGradeCount)

	if globalGradeCount == 0 {
		c.HTML(http.StatusOK, "no_grades.html", gin.H{"User": s, "AppName": initializers.AppName, "Subject": initializers.CurrentSubject})
		return
	}

	var displayGrades []models.Grade
	initializers.DB.Scopes(utils.FilterSubject).
		Where("student_id = ?", s.StudentID).
		Where("item_name NOT IN ?", IgnoredGradeItems).
		Order("id asc").
		Find(&displayGrades)

	preFinalTotal := 0.0
	finalIdx := -1
	for i, g := range displayGrades {
		if strings.EqualFold(g.ItemName, "Final") || strings.EqualFold(g.ItemName, "期末考") {
			finalIdx = i
		} else {
			preFinalTotal += g.Score
		}
	}

	if finalIdx != -1 {
		fWeight := 100.0 - preFinalTotal
		if fWeight < 0 { fWeight = 0 }
		rawFinal := displayGrades[finalIdx].Score
		weightedFinal := rawFinal * (fWeight / 100.0)

		displayGrades[finalIdx].ItemName = fmt.Sprintf("%s (原始:%g, 佔比:%.1f%%)", displayGrades[finalIdx].ItemName, rawFinal, fWeight)
		displayGrades[finalIdx].Score = math.Round(weightedFinal*100) / 100
	}

	var allClassGrades []models.Grade
	initializers.DB.Table("grades").
		Select("grades.*").
		Joins("JOIN rosters ON rosters.student_id = grades.student_id AND rosters.subject = grades.subject AND rosters.deleted_at IS NULL").
		Where("grades.subject = ?", initializers.CurrentSubject).
		Where("grades.item_name NOT IN ?", IgnoredGradeItems).
		Where("grades.deleted_at IS NULL").
		Find(&allClassGrades)

	studentPreFinal := make(map[string]float64)
	studentFinalRaw := make(map[string]float64)
	studentHasFinal := make(map[string]bool)

	for _, g := range allClassGrades {
		if strings.EqualFold(g.ItemName, "Final") || strings.EqualFold(g.ItemName, "期末考") {
			studentFinalRaw[g.StudentID] = g.Score
			studentHasFinal[g.StudentID] = true
		} else {
			studentPreFinal[g.StudentID] += g.Score
		}
	}

	var classTotals []float64
	myTotal := 0.0
	studentIDsMap := make(map[string]bool)
	for _, g := range allClassGrades { studentIDsMap[g.StudentID] = true }

	for sid := range studentIDsMap {
		pre := studentPreFinal[sid]
		total := pre
		if studentHasFinal[sid] {
			weight := 100.0 - pre
			if weight < 0 { weight = 0 }
			total += studentFinalRaw[sid] * (weight / 100.0)
		}
		total = math.Round(total*100) / 100
		classTotals = append(classTotals, total)
		if sid == s.StudentID { myTotal = total }
	}

	sum, minScore, maxScore := 0.0, 1000.0, -1.0
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
		if t >= myTotal {
			rank = i
			break
		}
		rank = i + 1
	}

	percentile := 0
	if len(classTotals) > 1 {
		p := math.Floor((float64(rank) / float64(len(classTotals))) * 100)
		percentile = int(p)
		if percentile > 99 { percentile = 99 }
	} else {
		percentile = 99
	}

	var top3 []float64
	count := len(classTotals)
	for i := count - 1; i >= 0 && len(top3) < 3; i-- {
		top3 = append(top3, classTotals[i])
	}

	finalWeight := 100.0 - preFinalTotal
	if finalWeight < 0 { finalWeight = 0 }

	c.HTML(200, "my_grades.html", gin.H{
		"User":        s,
		"Grades":      displayGrades,
		"MyTotal":     myTotal,
		"ClassMean":   mean,
		"ClassStdDev": stdDev,
		"ClassMin":    minScore,
		"ClassMax":    maxScore,
		"Percentile":  percentile,
		"Top3":        top3,
		"FinalWeight": finalWeight,
		"AppName":     initializers.AppName,
	})
}