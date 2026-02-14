package middleware

import (
	"fmt"
	"grade-system/initializers"
	"grade-system/models"
	"grade-system/utils"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// RequireTeacher 確保使用者是老師
func RequireTeacher(c *gin.Context) {
	session := sessions.Default(c)
	uid := session.Get("user_id")
	if uid == nil {
		c.Redirect(302, "/")
		c.Abort()
		return
	}
	isAdminSession := strings.HasPrefix(fmt.Sprintf("%v", uid), "ADMIN_")
	if !isAdminSession {
		var s models.Student
		if err := initializers.DB.Scopes(utils.FilterSubject).First(&s, uid).Error; err != nil || !utils.IsTeacher(s.Email) {
			c.String(403, "🚫 權限不足")
			c.Abort()
			return
		}
	}
	c.Next()
}