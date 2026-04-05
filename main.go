package main

import (
	"grade-system/router"
	"os"
)

func main() {
	// 初始化路由
	r := router.SetupRouter()

	// 嘗試獲取 Vercel (或雲端環境) 提供的 PORT 環境變數
	port := os.Getenv("PORT")
	
	// 如果找不到 PORT (代表在本地開發)，就預設使用 8080
	if port == "" {
		port = "8080"
	}

	// 啟動伺服器並監聽正確的通訊埠
	r.Run(":" + port)
}