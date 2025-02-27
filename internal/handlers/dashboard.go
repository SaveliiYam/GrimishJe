package handlers

import (
	"log"
	"net/http"
	"strconv"

	"github.com/MoshKillaPit/GrimishJe/internal/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Dashboard рендерит HTML-страницу личного кабинета с данными пользователя и его заказами.
func Dashboard(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Проверяем авторизацию через сессию
		session := sessions.Default(c)
		userIDVal := session.Get("user_id")
		if userIDVal == nil {
			log.Println("Неавторизованный доступ к личному кабинету")
			c.Redirect(http.StatusFound, "/")
			return
		}

		// Преобразуем user_id в uint
		var userID uint
		switch v := userIDVal.(type) {
		case uint:
			userID = v
		case int:
			userID = uint(v)
		case int64:
			userID = uint(v)
		case string:
			parsed, err := strconv.ParseUint(v, 10, 32)
			if err != nil {
				log.Printf("Ошибка преобразования user_id: %v", err)
				c.String(http.StatusInternalServerError, "Неверный формат user_id")
				return
			}
			userID = uint(parsed)
		default:
			log.Println("Неподдерживаемый тип user_id в сессии")
			c.String(http.StatusInternalServerError, "Неверный формат user_id")
			return
		}

		// Получаем данные пользователя из базы данных
		var user models.User
		if err := db.First(&user, userID).Error; err != nil {
			log.Printf("Ошибка получения данных пользователя с ID %d: %v", userID, err)
			c.String(http.StatusInternalServerError, "Ошибка получения данных пользователя")
			return
		}

		// Получаем заказы пользователя
		var orders []models.Order
		if err := db.Where("user_id = ?", userID).Find(&orders).Error; err != nil {
			log.Printf("Ошибка получения заказов для пользователя ID %d: %v", userID, err)
			c.String(http.StatusInternalServerError, "Ошибка получения заказов")
			return
		}

		log.Printf("Пользователь с ID %d успешно загрузил личный кабинет (заказов: %d)", userID, len(orders))

		// Рендерим шаблон личного кабинета с данными
		c.HTML(http.StatusOK, "dashboard.html", gin.H{
			"title":  "Личный кабинет",
			"User":   user,
			"Orders": orders,
		})
	}
}
