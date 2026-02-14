package initializers

import (
	"fmt"
	"log"
	"os"

	"grade-system/models" // 確保 module 名稱對應 go.mod

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

func ConnectToDB() {
	var err error
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Taipei",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))

	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("資料庫連線失敗: ", err)
	}

	// 自動遷移
	DB.AutoMigrate(&models.Student{}, &models.Grade{}, &models.Roster{})
}