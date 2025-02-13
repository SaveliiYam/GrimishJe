package handlers

import (
	"net/http"

	"github.com/MoshKillaPit/GrimishJe/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CreateOrder обрабатывает создание нового заказа.
func CreateOrder(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			Title       string  `json:"title" binding:"required"`
			Description string  `json:"description"`
			Price       float64 `json:"price" binding:"required"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Для примера используется фиксированный userID = 1
		order := models.Order{
			UserID:      1,
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
