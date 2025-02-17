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
		// Извлекаем все заказы из базы данных
		var orders []models.Order
		if err := db.Find(&orders).Error; err != nil {
			c.String(http.StatusInternalServerError, "Ошибка получения заказов")
			return
		}

		// Извлекаем данные пользователя (админа) из сессии
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

		// Рендерим шаблон admin_dashboard.html, передавая пользователя и все заказы
		c.HTML(http.StatusOK, "admin_dashboard.html", gin.H{
			"User":   user,
			"Orders": orders,
		})
	}
}
