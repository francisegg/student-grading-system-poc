package initializers

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var (
	GoogleOauthConfig *oauth2.Config
	CurrentSubject    string
	IsAdminMode       bool
	AppName           string
)

func LoadEnvVariables() {
	if err := godotenv.Load(); err != nil {
		log.Println("找不到 .env 檔案，使用系統環境變數")
	}
}

func InitConfig() {
	CurrentSubject = os.Getenv("APP_SUBJECT")
	AppName = os.Getenv("APP_NAME")
	if AppName == "" {
		AppName = "學生分數平台"
	}
	if os.Getenv("APP_MODE") == "admin" {
		IsAdminMode = true
		AppName = "教師總管理後台"
	}

	GoogleOauthConfig = &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("GOOGLE_REDIRECT_URL"),
		Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile"},
		Endpoint:     google.Endpoint,
	}
}