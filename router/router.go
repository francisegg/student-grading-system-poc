package router

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"os"

	"grade-system/controllers"
	"grade-system/initializers"
	"grade-system/middleware"
	"grade-system/utils"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

//go:embed templates/*
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// SetupRouter 初始化並回傳設定好的 Gin 引擎
func SetupRouter() *gin.Engine {
	initializers.LoadEnvVariables()
	initializers.ConnectToDB()
	initializers.InitConfig()

	r := gin.Default()

	// 1. 設定靜態檔案 (使用 embed，不依賴外部資料夾)
	subStatic, _ := fs.Sub(staticFS, "static")
	r.StaticFS("/static", http.FS(subStatic))

	// 2. 設定 HTML 樣板 (使用 embed，不依賴外部資料夾)
	templ := template.Must(template.New("").Funcs(template.FuncMap{
		"inc": utils.Inc,
	}).ParseFS(templatesFS, "templates/*"))
	r.SetHTMLTemplate(templ)

	// Session 設定
	store := cookie.NewStore([]byte(os.Getenv("SESSION_SECRET")))
	r.Use(sessions.Sessions("mysession", store))

	// --- 路由設定 ---
	r.GET("/", controllers.ShowIndex)
	r.GET("/login", controllers.Login)
	r.GET("/auth/callback", controllers.Callback)
	r.GET("/logout", controllers.Logout)

	r.GET("/register", controllers.ShowRegister)
	r.POST("/register", controllers.Register)
	r.GET("/my-grades", controllers.ShowMyGrades)

	teacher := r.Group("/teacher")
	teacher.Use(middleware.RequireTeacher)
	{
		teacher.GET("/dashboard", controllers.TeacherDashboard)
		teacher.POST("/upload", controllers.UploadGrades)
		teacher.POST("/upload-roster", controllers.UploadRoster)

		teacher.POST("/roster/post", controllers.PostRoster)
		teacher.POST("/grade/post", controllers.PostGrade)
		teacher.GET("/grade/delete", controllers.DeleteGrade)
		teacher.GET("/roster/delete-one", controllers.DeleteSingleRoster)
		teacher.GET("/student/unbind", controllers.UnbindStudentEmail)

		teacher.POST("/delete-roster", controllers.ClearRoster)
		teacher.POST("/delete-all", controllers.ClearAllGrades)
	}

	return r
}