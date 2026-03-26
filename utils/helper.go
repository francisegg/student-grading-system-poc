package utils

import (
	"grade-system/initializers"
	"os"
	"strings"

	"gorm.io/gorm"
)

// CleanHeader 清理 CSV 標題的 BOM 與空白
func CleanHeader(h string) string {
	h = strings.ReplaceAll(h, "\ufeff", "")
	return strings.TrimSpace(h)
}

// CleanID 徹底清理學號中的空白、BOM 與換行
func CleanID(id string) string {
	id = strings.ReplaceAll(id, "\ufeff", "")
	return strings.TrimSpace(id)
}

// Inc 樣板用的加法函式
func Inc(i int) int {
	return i + 1
}

// IsTeacher 檢查是否為老師
func IsTeacher(email string) bool {
	whitelist := os.Getenv("TEACHER_WHITELIST")
	return strings.Contains(whitelist, email)
}

// FilterSubject GORM Scope: 自動過濾科目
func FilterSubject(db *gorm.DB) *gorm.DB {
	if initializers.CurrentSubject != "" {
		return db.Where("subject = ?", initializers.CurrentSubject)
	}
	return db
}