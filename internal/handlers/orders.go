package handlers

import (
	"net/http"
	"strconv"

	"github.com/MoshKillaPit/GrimishJe/internal/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CreateOrder – обрабатывает создание нового заказа.
// Обратите внимание: используется ShouldBind, чтобы поддерживать и JSON, и form data.
func CreateOrder(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			Title       string  `form:"title" binding:"required"`
			Description string  `form:"description"`
			Price       float64 `form:"price" binding:"required"`
		}
		if err := c.ShouldBind(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Извлекаем user_id из сессии
		session := sessions.Default(c)
		uid := session.Get("user_id")
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
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Невалидный user_id"})
				return
			}
			userID = uint(parsed)
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Невалидный user_id"})
			return
		}

		order := models.Order{
			UserID:      userID,
			Title:       input.Title,
			Description: input.Description,
			Price:       input.Price,
			Status:      "новый",
		}
		if err := db.Create(&order).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось создать заказ"})
			return
		}

		c.JSON(http.StatusCreated, gin.H{"message": "Заказ успешно создан", "order": order})
	}
}
