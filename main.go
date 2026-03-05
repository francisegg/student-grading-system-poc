package main

import (
	"html/template"
	"os"

	"grade-system/controllers"
	"grade-system/initializers"
	"grade-system/middleware"
	"grade-system/utils"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func init() {
	initializers.LoadEnvVariables()
	initializers.ConnectToDB()
	initializers.InitConfig()
}

func main() {
	r := gin.Default()

	r.Static("/static", "./static")

	r.SetFuncMap(template.FuncMap{
		"inc": utils.Inc,
	})

	store := cookie.NewStore([]byte(os.Getenv("SESSION_SECRET")))
	r.Use(sessions.Sessions("mysession", store))
	r.LoadHTMLGlob("templates/*")

	// --- 1. 首頁與認證 ---
	r.GET("/", controllers.ShowIndex)
	r.GET("/login", controllers.Login)
	r.GET("/auth/callback", controllers.Callback)
	r.GET("/logout", controllers.Logout)

	// --- 2. 註冊 ---
	r.GET("/register", controllers.ShowRegister)
	r.POST("/register", controllers.Register)

	// --- 3. 學生查詢 ---
	r.GET("/my-grades", controllers.ShowMyGrades)

	// --- 4. 老師後台 ---
	teacher := r.Group("/teacher")
	teacher.Use(middleware.RequireTeacher)
	{
		teacher.GET("/dashboard", controllers.TeacherDashboard)
		teacher.POST("/upload", controllers.UploadGrades)
		teacher.POST("/upload-roster", controllers.UploadRoster)
		
		// 手動管理路由
		teacher.POST("/roster/post", controllers.PostRoster)
		teacher.POST("/grade/post", controllers.PostGrade)
		teacher.GET("/grade/delete", controllers.DeleteGrade)

		// 單筆刪除與解綁
		teacher.GET("/roster/delete-one", controllers.DeleteSingleRoster)
		teacher.GET("/student/unbind", controllers.UnbindStudentEmail)

		// 批次清空路由
		teacher.POST("/delete-roster", controllers.ClearRoster)
		teacher.POST("/delete-all", controllers.ClearAllGrades)
	}

	r.Run(":8080")
}