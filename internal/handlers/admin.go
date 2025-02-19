package handlers

import (
	"net/http"

	"github.com/MoshKillaPit/GrimishJe/internal/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AdminDashboard рендерит админ-панель, где администратор видит все заказы.
func AdminDashboard(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var orders []models.Order
		// Используем Preload, чтобы загрузить связанные данные пользователя
		if err := db.Preload("User").Find(&orders).Error; err != nil {
			c.String(http.StatusInternalServerError, "Ошибка получения заказов")
			return
		}

		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil {
			c.Redirect(http.StatusFound, "/")
			return
		}
		var user models.User
		if err := db.First(&user, uid).Error; err != nil {
			c.String(http.StatusInternalServerError, "Ошибка получения данных пользователя")
			return
		}

		c.HTML(http.StatusOK, "admin_dashboard.html", gin.H{
			"User":   user,
			"Orders": orders,
		})
	}
}
