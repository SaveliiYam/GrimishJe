package handlers

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/MoshKillaPit/GrimishJe/internal/models"
	"github.com/MoshKillaPit/GrimishJe/internal/websocket"
)

// GetAdminOrder рендерит отдельную страницу с деталями заказа для администратора.
// URL: /admin/order/:id
func GetAdminOrder(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Получаем orderID из параметров пути
		idStr := c.Param("id")
		orderID, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			log.Printf("GetAdminOrder: Неверный формат orderID: %v", err)
			c.String(http.StatusBadRequest, "Неверный orderID")
			return
		}

		// Получаем заказ с предварительной загрузкой связанных данных: User и Reviews
		var order models.Order
		if err := db.Preload("User").Preload("Reviews").First(&order, uint(orderID)).Error; err != nil {
			log.Printf("GetAdminOrder: Заказ с ID %d не найден: %v", orderID, err)
			c.String(http.StatusNotFound, "Заказ не найден")
			return
		}

		// Получаем файлы, связанные с заказом
		var files []models.File
		if err := db.Where("order_id = ?", order.ID).Find(&files).Error; err != nil {
			log.Printf("GetAdminOrder: Ошибка получения файлов для заказа %d: %v", orderID, err)
			// Продолжаем рендеринг, даже если файлы не удалось загрузить
		}

		// Рендерим шаблон детальной страницы заказа для админа (admin_order.html)
		c.HTML(http.StatusOK, "admin_order.html", gin.H{
			"Order": order,
			"User":  order.User,
			"Files": files,
		})
	}
}

func MarkOrderPaid(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			OrderID      string  `form:"orderID" binding:"required"`
			ExtraPayment float64 `form:"extraPayment"` // Дополнительная сумма (может быть 0)
		}

		if err := c.ShouldBind(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверные данные: " + err.Error()})
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

		// Обновляем статус оплаты и доплату
		order.IsPaid = true
		order.ExtraPayment = input.ExtraPayment

		if err := db.Save(&order).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить заказ"})
			return
		}

		// Отправляем уведомление через WebSocket
		payload := websocket.PaymentUpdatePayload{
			OrderID:      order.ID,
			IsPaid:       order.IsPaid,
			ExtraPayment: order.ExtraPayment,
		}
		websocket.BroadcastMessage(websocket.GetHub(), payload)

		c.JSON(http.StatusOK, gin.H{"message": "Заказ отмечен как оплаченный", "order": order})
	}
}
