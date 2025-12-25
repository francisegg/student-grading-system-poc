package initializers

import (
	"fmt"
	"log"
	"os"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// DB 是一個全域變數，讓其他 package (如 controllers, models) 可以直接使用
var DB *gorm.DB

// ConnectToDB 負責建立與 PostgreSQL 的連線
func ConnectToDB() {
	var err error

	// 從環境變數讀取連線字串 (DSN)
	dsn := os.Getenv("DB_URL") 
    // 假設 .env 裡是: DB_URL="host=localhost user=postgres password=password dbname=grades port=5432 sslmode=disable"
	
	if dsn == "" {
		log.Fatal("DB_URL not found in .env file")
	}

	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})

	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	fmt.Println("Successfully connected to the database!")
}