package models

import "gorm.io/gorm"

// Student 代表學生帳號資訊
type Student struct {
    gorm.Model
    StudentID string `gorm:"uniqueIndex:idx_sid_subject"` 
    Name      string                       
    Class     string                       
    Email     string `gorm:"uniqueIndex:idx_email_subject"` 
    Subject   string `gorm:"uniqueIndex:idx_sid_subject;uniqueIndex:idx_email_subject"` 
}

type Grade struct {
	gorm.Model
	StudentID string  `gorm:"index:idx_grade_item_subject,unique"` 
	ItemName  string  `gorm:"index:idx_grade_item_subject,unique"`
	Score     float64 
	Subject   string  `gorm:"index:idx_grade_item_subject,unique;not null"`
}

// Roster 用於記錄上傳的名單原始資料
type Roster struct {
    gorm.Model
    StudentID string `gorm:"uniqueIndex:idx_roster_sid_subject"`
    Name      string // 預留欄位 (若 CSV 有名字可用)
    Class     string // 班級
    Subject   string `gorm:"uniqueIndex:idx_roster_sid_subject"`
}