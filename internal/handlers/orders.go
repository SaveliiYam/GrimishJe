package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/MoshKillaPit/GrimishJe/internal/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CreateOrder создаёт новый заказ со статусом "новый".
func CreateOrder(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			Topic              string  `form:"topic" binding:"required"`
			Description        string  `form:"description" binding:"required"`
			Deadline           string  `form:"deadline" binding:"required"`
			PlagiarismRequired bool    `form:"plagiarismRequired"`
			PlagiarismPercent  uint    `form:"plagiarismPercent"`
			Budget             float64 `form:"budget" binding:"required"`
			WorkType           string  `form:"workType" binding:"required"`
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
			UserID:             userID,
			Topic:              input.Topic,
			Description:        input.Description,
			Deadline:           input.Deadline,
			PlagiarismRequired: input.PlagiarismRequired,
			PlagiarismPercent:  input.PlagiarismPercent,
			Budget:             input.Budget,
			Status:             "новый", // Новый заказ создаётся со статусом "новый"
			WorkType:           input.WorkType,
		}

		// Генерация OrderNumber
		// Формат: ORD-YYYYMMDD-XXX
		today := time.Now().Format("20060102")
		var count int64
		// Считаем, сколько заказов уже создано с префиксом ORD-YYYYMMDD-
		if err := db.Model(&models.Order{}).
			Where("order_number LIKE ?", "ORD-"+today+"-%").
			Count(&count).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при генерации номера заказа"})
			return
		}
		order.OrderNumber = fmt.Sprintf("ORD-%s-%03d", today, count+1)

		if err := db.Create(&order).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось создать заказ"})
			return
		}

		c.JSON(http.StatusCreated, gin.H{"message": "Заказ успешно создан", "orderId": order.ID})
	}
}

// EditOrder позволяет владельцу заказа обновлять сроки и заметки.
func EditOrder(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			OrderID  string `form:"orderID" binding:"required"`
			Deadline string `form:"deadline" binding:"required"`
			Notes    string `form:"notes"`
		}

		if err := c.ShouldBind(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		orderID, err := strconv.ParseUint(input.OrderID, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
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

		var order models.Order
		if err := db.First(&order, uint(orderID)).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		if order.UserID != userID {
			c.JSON(http.StatusForbidden, gin.H{"error": "Нет доступа к этому заказу"})
			return
		}

		order.Deadline = input.Deadline
		order.Notes = input.Notes

		if err := db.Save(&order).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить заказ"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Заказ успешно обновлён"})
	}
}

// AcceptOrder переводит заказ в статус "в работе". (Администратор)
func AcceptOrder(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			OrderID string `form:"orderID" binding:"required"`
		}
		if err := c.ShouldBind(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		orderID, err := strconv.ParseUint(input.OrderID, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
			return
		}
		var order models.Order
		if err := db.First(&order, uint(orderID)).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		order.Status = "в работе"
		if err := db.Save(&order).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить заказ"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Заказ принят, статус обновлён на 'в работе'"})
	}
}

// CompleteOrder переводит заказ в статус "завершён". (Администратор)
func CompleteOrder(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			OrderID string `form:"orderID" binding:"required"`
		}
		if err := c.ShouldBind(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		orderID, err := strconv.ParseUint(input.OrderID, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
			return
		}
		var order models.Order
		if err := db.First(&order, uint(orderID)).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		order.Status = "завершён"
		if err := db.Save(&order).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить заказ"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Заказ завершён"})
	}
}

// CancelOrder переводит заказ в статус "отменён". (Администратор)
func CancelOrder(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			OrderID string `form:"orderID" binding:"required"`
		}
		if err := c.ShouldBind(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		orderID, err := strconv.ParseUint(input.OrderID, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
			return
		}
		var order models.Order
		if err := db.First(&order, uint(orderID)).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		order.Status = "отменён"
		if err := db.Save(&order).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить заказ"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Заказ отменён"})
	}
}

// DeleteOrder удаляет заказ из базы. (Администратор)
func DeleteOrder(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			OrderID string `form:"orderID" binding:"required"`
		}
		if err := c.ShouldBind(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		orderID, err := strconv.ParseUint(input.OrderID, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
			return
		}

		if err := db.Delete(&models.Order{}, uint(orderID)).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось удалить заказ"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Заказ удалён"})
	}
}
