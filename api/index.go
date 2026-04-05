package main
import (
	"net/http"
	"grade-system/router"
	"github.com/gin-gonic/gin"
)

var app *gin.Engine

func init() {
	// Vercel 啟動 Function 時會執行 init()
	app = router.SetupRouter()
}

// Handler 是 Vercel 要求的標準 HTTP 進入點
func Handler(w http.ResponseWriter, r *http.Request) {
	app.ServeHTTP(w, r)
}