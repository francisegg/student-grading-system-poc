package initializers

import (
	"log"
	"os"

	"grade-system/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

func ConnectToDB() {
	var err error
	
	// 優先讀取 Neon (或 Vercel) 提供的完整連線字串
	dsn := os.Getenv("DATABASE_URL")
	
	// 如果沒有 DATABASE_URL，則退回使用原本的拆分環境變數 (供本地開發備用)
	if dsn == "" {
		dsn = "host=" + os.Getenv("DB_HOST") + 
			  " user=" + os.Getenv("DB_USER") + 
			  " password=" + os.Getenv("DB_PASSWORD") + 
			  " dbname=" + os.Getenv("DB_NAME") + 
			  " port=" + os.Getenv("DB_PORT") + 
			  " sslmode=disable TimeZone=Asia/Taipei"
	}

	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("資料庫連線失敗: ", err)
	}

	// 自動遷移
	DB.AutoMigrate(&models.Student{}, &models.Grade{}, &models.Roster{})
}