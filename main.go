package main

import (
	"grade-system/router"
)

func main() {
	// 本地開發時的進入點
	r := router.SetupRouter()
	r.Run(":8080")
}