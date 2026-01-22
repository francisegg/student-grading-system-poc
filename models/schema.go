package models

import "gorm.io/gorm"

type Student struct {
	gorm.Model
	// 修改：Email 和 StudentID 不再是全域唯一，而是要在該 Subject 下唯一
	Email     string `gorm:"index:idx_email_subject,unique"` 
	Name      string
	StudentID string `gorm:"index:idx_stu_subject,unique"` 
	Course    string // 這是班級 (例如甲班)
	
	// ★ 新增：科目 (例如 circuit, antenna)
	Subject   string `gorm:"index:idx_email_subject,unique;index:idx_stu_subject,unique;not null"`
}

type Grade struct {
	gorm.Model
	StudentID string  `gorm:"index:idx_grade_item_subject,unique"` 
	ItemName  string  `gorm:"index:idx_grade_item_subject,unique"`
	Score     float64 
	
	// ★ 新增：科目
	Subject   string  `gorm:"index:idx_grade_item_subject,unique;not null"`
}