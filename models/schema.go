package models

import "gorm.io/gorm"

// Student 代表學生帳號資訊 (用 Google 登入註冊的資料)
type Student struct {
	gorm.Model
	StudentID string `gorm:"uniqueIndex:idx_sid_subject"`
	Name      string
	Class     string
	Email     string `gorm:"uniqueIndex:idx_email_subject"`
	Subject   string `gorm:"uniqueIndex:idx_sid_subject;uniqueIndex:idx_email_subject"`
}

// Grade 代表單一成績紀錄
type Grade struct {
	gorm.Model
	StudentID string  `gorm:"index:idx_grade_item_subject,unique"`
	ItemName  string  `gorm:"index:idx_grade_item_subject,unique"`
	Score     float64 
	Subject   string  `gorm:"index:idx_grade_item_subject,unique;not null"`
}

// Roster 用於記錄老師上傳的名單原始資料
type Roster struct {
	gorm.Model
	StudentID string `gorm:"uniqueIndex:idx_roster_sid_subject"`
	Name      string // 🌟 新增：存取 CSV 中的姓名
	Class     string
	Subject   string `gorm:"uniqueIndex:idx_roster_sid_subject"`
}