package controllers

import (
	"context"
	"encoding/json"
	"grade-system/initializers"
	"grade-system/models"
	"grade-system/utils"
	"io/ioutil"
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func Login(c *gin.Context) {
	url := initializers.GoogleOauthConfig.AuthCodeURL("state")
	c.Redirect(http.StatusTemporaryRedirect, url)
}

func Callback(c *gin.Context) {
	token, err := initializers.GoogleOauthConfig.Exchange(context.Background(), c.Query("code"))
	if err != nil {
		c.Redirect(302, "/")
		return
	}

	resp, _ := http.Get("https://www.googleapis.com/oauth2/v2/userinfo?access_token=" + token.AccessToken)
	defer resp.Body.Close()
	data, _ := ioutil.ReadAll(resp.Body)

	var gUser struct{ Email, Name string }
	json.Unmarshal(data, &gUser)
	session := sessions.Default(c)

	if initializers.IsAdminMode {
		if !utils.IsTeacher(gUser.Email) {
			c.String(403, "🚫 抱歉，只有老師可以登入此後台。")
			return
		}
		session.Set("user_id", "ADMIN_"+gUser.Email)
		session.Save()
		c.Redirect(http.StatusSeeOther, "/")
		return
	}

	var s models.Student
	result := initializers.DB.Scopes(utils.FilterSubject).Where("email = ?", gUser.Email).First(&s)

	if result.Error == gorm.ErrRecordNotFound || s.StudentID == "" {
		session.Set("temp_email", gUser.Email)
		session.Set("temp_name", gUser.Name)
		session.Save()
		c.Redirect(http.StatusSeeOther, "/register")
		return
	}

	session.Set("user_id", s.ID)
	session.Save()
	c.Redirect(http.StatusSeeOther, "/")
}

func Logout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	session.Save()
	c.Redirect(302, "/")
}