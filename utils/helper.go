package utils

import (
	"grade-system/initializers"
	"os"
	"strings"

	"gorm.io/gorm"
)

// 清理 BOM 與空白
func CleanHeader(h string) string {
	h = strings.ReplaceAll(h, "\ufeff", "")
	return strings.TrimSpace(h)
}

// 樣板用的加法函式
func Inc(i int) int {
	return i + 1
}

// 檢查是否為老師
func IsTeacher(email string) bool {
	whitelist := os.Getenv("TEACHER_WHITELIST")
	return strings.Contains(whitelist, email)
}

// GORM Scope: 自動過濾科目
func FilterSubject(db *gorm.DB) *gorm.DB {
	if initializers.CurrentSubject != "" {
		return db.Where("subject = ?", initializers.CurrentSubject)
	}
	return db
}