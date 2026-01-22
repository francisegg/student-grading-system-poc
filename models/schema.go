package models

import "gorm.io/gorm"

type Student struct {
	gorm.Model
	Email     string `gorm:"index:idx_email_subject,unique"` 
	Name      string
	StudentID string `gorm:"index:idx_stu_subject,unique"` 
	Course    string // 班級
	Subject   string `gorm:"index:idx_email_subject,unique;index:idx_stu_subject,unique;not null"`
}

type Grade struct {
	gorm.Model
	StudentID string  `gorm:"index:idx_grade_item_subject,unique"` 
	ItemName  string  `gorm:"index:idx_grade_item_subject,unique"`
	Score     float64 
	Subject   string  `gorm:"index:idx_grade_item_subject,unique;not null"`
}

// ★ 新增：修課名單 (白名單)
type Roster struct {
	gorm.Model
	StudentID string `gorm:"index:idx_roster_id_subject,unique"` // 學號
	Name      string // 學生真實姓名
	Course    string // 班級
	Subject   string `gorm:"index:idx_roster_id_subject,unique;not null"`
}