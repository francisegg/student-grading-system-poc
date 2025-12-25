package models

import "gorm.io/gorm"

type Student struct {
	gorm.Model
	Email     string `gorm:"uniqueIndex;not null"`
	Name      string
	StudentID string `gorm:"uniqueIndex"`
	Course    string
}

type Grade struct {
	gorm.Model
	// 設定複合唯一索引：這兩個欄位加起來必須是唯一的
	StudentID string  `gorm:"index;uniqueIndex:idx_student_item"` 
	ItemName  string  `gorm:"uniqueIndex:idx_student_item"`
	Score     float64 
}