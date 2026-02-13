package models

import "gorm.io/gorm"

// Student 代表學生帳號資訊
type Student struct {
    gorm.Model
    StudentID string `gorm:"uniqueIndex"` // 對應 CSV 的 ID
    Name      string                       // 學生姓名 (可由註冊或名單提供)
    Class     string                       // 對應 CSV 的 Class
    Email     string `gorm:"uniqueIndex"` // 用於 Google 登入對應
}

type Grade struct {
	gorm.Model
	StudentID string  `gorm:"index:idx_grade_item_subject,unique"` 
	ItemName  string  `gorm:"index:idx_grade_item_subject,unique"`
	Score     float64 
	Subject   string  `gorm:"index:idx_grade_item_subject,unique;not null"`
}

// Roster 用於記錄上傳的名單原始資料 (可選)
type Roster struct {
    gorm.Model
    StudentID string
    Class     string
    Subject   string // 區分是哪個課程的名單
}