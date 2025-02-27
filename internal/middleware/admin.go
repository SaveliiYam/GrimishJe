package middleware

import (
	"log"
	"net/http"
	"strconv"

	"github.com/MoshKillaPit/GrimishJe/internal/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AdminRequired проверяет, что пользователь авторизован и имеет роль администратора.
func AdminRequired(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Получаем сессию
		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil {
			log.Println("Неавторизованный доступ к ресурсу, требующему роли администратора")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Необходимо авторизоваться"})
			c.Abort()
			return
		}

		// Преобразуем user_id в uint
		var userID uint
		switch v := uid.(type) {
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
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Неверный формат user_id"})
				c.Abort()
				return
			}
			userID = uint(parsed)
		default:
			log.Println("Неподдерживаемый тип user_id в сессии")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Неверный формат user_id"})
			c.Abort()
			return
		}

		// Проверяем, что пользователь существует и является администратором
		var user models.User
		if err := db.First(&user, userID).Error; err != nil {
			log.Printf("Ошибка получения данных пользователя с ID %d: %v", userID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка получения данных пользователя"})
			c.Abort()
			return
		}

		if !user.IsAdmin {
			log.Printf("Попытка доступа к ресурсу неадминистратором (UserID: %d)", userID)
			c.JSON(http.StatusForbidden, gin.H{"error": "Доступ разрешён только администраторам"})
			c.Abort()
			return
		}

		log.Printf("Администратор с ID %d успешно прошёл проверку доступа, сессия: %+v", userID, session)
		c.Next()
	}
}
