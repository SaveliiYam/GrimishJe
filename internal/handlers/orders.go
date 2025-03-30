package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/MoshKillaPit/GrimishJe/internal/models"
	"github.com/MoshKillaPit/GrimishJe/internal/websocket"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

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
			Budget             float64 `form:"budget" binding:"required,gt=0"`
			WorkType           string  `form:"workType" binding:"required"`
		}

		if bindErr := c.ShouldBind(&input); bindErr != nil {
			log.Printf("CreateOrder: Ошибка привязки данных: %v", bindErr)
			if validationErr, ok := bindErr.(validator.ValidationErrors); ok {
				for _, e := range validationErr {
					if e.Field() == "Budget" && e.Tag() == "gt" {
						c.JSON(http.StatusBadRequest, gin.H{"error": "Бюджет должен быть больше 0"})
						return
					}
				}
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверные данные формы"})
			return
		}

		// Валидация дедлайна
		var deadline time.Time
		var parseErr error
		layouts := []string{"2006-01-02", "2006-01-02T15:04:05Z"}
		for _, layout := range layouts {
			deadline, parseErr = time.Parse(layout, input.Deadline)
			if parseErr == nil {
				break
			}
		}
		if parseErr != nil {
			log.Printf("CreateOrder: Неверный формат дедлайна для заказа: %s", input.Deadline)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат дедлайна (ожидается YYYY-MM-DD или YYYY-MM-DDTHH:MM:SSZ)"})
			return
		}
		input.Deadline = deadline.Format("2006-01-02")

		currentTime := time.Now()
		if !deadline.After(currentTime) {
			log.Printf("CreateOrder: Дедлайн (%s) не может быть раньше текущей даты (%s)",
				input.Deadline, currentTime.Format("2006-01-02"))
			c.JSON(http.StatusBadRequest, gin.H{"error": "Срок выполнения должен быть не раньше сегодняшнего дня"})
			return
		}

		if input.PlagiarismRequired && input.PlagiarismPercent > 100 {
			log.Printf("CreateOrder: Неверное значение PlagiarismPercent: %d", input.PlagiarismPercent)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Процент плагиата должен быть не более 100"})
			return
		}

		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil {
			log.Println("Неавторизованный доступ при создании заказа")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Необходимо авторизоваться"})
			return
		}

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
				log.Printf("CreateOrder: Ошибка преобразования user_id: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Неверный user_id"})
				return
			}
			userID = uint(parsed)
		default:
			log.Println("CreateOrder: Неподдерживаемый тип user_id в сессии")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Неверный user_id"})
			return
		}

		// Формируем уникальный номер заказа с использованием даты и UnixNano
		orderNumber := fmt.Sprintf("ORD-%s-%d", time.Now().Format("20060102"), time.Now().UnixNano())

		order := models.Order{
			OrderNumber:        orderNumber,
			UserID:             userID,
			Topic:              input.Topic,
			Description:        input.Description,
			Deadline:           input.Deadline,
			PlagiarismRequired: input.PlagiarismRequired,
			PlagiarismPercent:  input.PlagiarismPercent,
			Budget:             input.Budget,
			Status:             "новый",
			WorkType:           input.WorkType,
		}

		if err := db.Create(&order).Error; err != nil {
			log.Printf("CreateOrder: Ошибка создания заказа для пользователя %d: %v", userID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось создать заказ"})
			return
		}

		log.Printf("Создан новый заказ для пользователя %d (OrderID: %d, OrderNumber: %s)",
			userID, order.ID, order.OrderNumber)

		c.JSON(http.StatusCreated, gin.H{
			"message":  "Заказ успешно создан",
			"orderId":  order.ID,
			"orderNum": order.OrderNumber,
		})
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

		if bindErr := c.ShouldBind(&input); bindErr != nil {
			log.Printf("EditOrder: Ошибка привязки данных: %v", bindErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверные данные формы: " + bindErr.Error()})
			return
		}

		orderID, parseErr := strconv.ParseUint(input.OrderID, 10, 64)
		if parseErr != nil {
			log.Printf("EditOrder: Неверный format orderID: %v", parseErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
			return
		}

		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil {
			log.Println("Неавторизованный доступ при редактировании заказа")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Необходимо авторизоваться"})
			return
		}

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
				log.Printf("EditOrder: Ошибка преобразования user_id: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Невалидный user_id"})
				return
			}
			userID = uint(parsed)
		default:
			log.Println("EditOrder: Неподдерживаемый тип user_id в сессии")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Неверный user_id"})
			return
		}

		var order models.Order
		if err := db.First(&order, uint(orderID)).Error; err != nil {
			log.Printf("EditOrder: Заказ с ID %d не найден: %v", orderID, err)
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		if order.UserID != userID {
			log.Printf("Пользователь %d попытался редактировать чужой заказ %d", userID, orderID)
			c.JSON(http.StatusForbidden, gin.H{"error": "Нет доступа к этому заказу"})
			return
		}

		var deadline time.Time
		layouts := []string{"2006-01-02", "2006-01-02T15:04:05Z"}
		for _, layout := range layouts {
			deadline, parseErr = time.Parse(layout, input.Deadline)
			if parseErr == nil {
				break
			}
		}
		if parseErr != nil {
			log.Printf("EditOrder: Неверный формат дедлайна для заказа %d: %s", orderID, input.Deadline)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат дедлайна (ожидается YYYY-MM-DD или YYYY-MM-DDTHH:MM:SSZ)"})
			return
		}
		input.Deadline = deadline.Format("2006-01-02")

		order.Deadline = input.Deadline
		order.Notes = input.Notes
		order.UpdatedAt = time.Now()

		if err := db.Save(&order).Error; err != nil {
			log.Printf("EditOrder: Ошибка обновления заказа %d для пользователя %d: %v", orderID, userID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить заказ"})
			return
		}

		log.Printf("Заказ %d успешно обновлён пользователем %d", orderID, userID)
		c.JSON(http.StatusOK, gin.H{"message": "Заказ успешно обновлён"})
	}
}

// AcceptOrder переводит заказ в статус "в работе". (Администратор)
func AcceptOrder(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			OrderID string `form:"orderID" binding:"required"`
		}

		if bindErr := c.ShouldBind(&input); bindErr != nil {
			log.Printf("AcceptOrder: Ошибка привязки данных: %v", bindErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверные данные формы: " + bindErr.Error()})
			return
		}

		orderID, parseErr := strconv.ParseUint(input.OrderID, 10, 64)
		if parseErr != nil {
			log.Printf("AcceptOrder: Неверный format orderID: %v", parseErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
			return
		}

		var order models.Order
		if err := db.First(&order, uint(orderID)).Error; err != nil {
			log.Printf("AcceptOrder: Заказ с ID %d не найден: %v", orderID, err)
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		if err := db.Model(&order).Update("status", "в работе").Error; err != nil {
			log.Printf("AcceptOrder: Ошибка обновления статуса заказа %d: %v", orderID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить заказ"})
			return
		}

		log.Printf("Заказ %d переведён в статус 'в работе'", orderID)

		// Отправка уведомления через WebSocket (статус)
		payload := websocket.OrderStatusUpdatePayload{
			OrderID: order.ID,
			Status:  "в работе",
			At:      time.Now().Unix(),
		}
		websocket.BroadcastMessage(websocket.GetHub(), payload)

		// --- Отправка уведомления в чат о смене статуса ---
		chatNotification := websocket.ChatMessagePayload{
			OrderID:    order.ID,
			Sender:     "system",
			Message:    "Статус заказа изменился на: в работе",
			CreatedAt:  time.Now().Unix(),
			UploadedBy: "system",
		}
		if err := db.Create(&models.ChatMessage{
			OrderID:   chatNotification.OrderID,
			Sender:    chatNotification.Sender,
			Message:   chatNotification.Message,
			CreatedAt: time.Unix(chatNotification.CreatedAt, 0),
		}).Error; err != nil {
			log.Printf("AcceptOrder: Ошибка сохранения уведомления чата для заказа %d: %v", orderID, err)
		}
		websocket.BroadcastMessage(websocket.GetHub(), chatNotification)
		// --- Конец блока уведомления в чат ---

		c.JSON(http.StatusOK, gin.H{"message": "Заказ принят, статус обновлён на 'в работе'"})
	}
}

// CompleteOrder переводит заказ в статус "завершён". (Администратор)
func CompleteOrder(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			OrderID string `form:"orderID" binding:"required"`
		}

		if bindErr := c.ShouldBind(&input); bindErr != nil {
			log.Printf("CompleteOrder: Ошибка привязки данных: %v", bindErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверные данные формы: " + bindErr.Error()})
			return
		}

		orderID, parseErr := strconv.ParseUint(input.OrderID, 10, 64)
		if parseErr != nil {
			log.Printf("CompleteOrder: Неверный format orderID: %v", parseErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
			return
		}

		var order models.Order
		if err := db.First(&order, uint(orderID)).Error; err != nil {
			log.Printf("CompleteOrder: Заказ с ID %d не найден: %v", orderID, err)
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		if err := db.Model(&order).Update("status", "завершён").Error; err != nil {
			log.Printf("CompleteOrder: Ошибка обновления статуса заказа %d: %v", orderID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить заказ"})
			return
		}

		log.Printf("Заказ %d переведён в статус 'завершён'", orderID)

		// Отправка уведомления через WebSocket (статус)
		payload := websocket.OrderStatusUpdatePayload{
			OrderID: order.ID,
			Status:  "завершён",
			At:      time.Now().Unix(),
		}
		websocket.BroadcastMessage(websocket.GetHub(), payload)

		// --- Отправка уведомления в чат о смене статуса ---
		chatNotification := websocket.ChatMessagePayload{
			OrderID:    order.ID,
			Sender:     "system",
			Message:    "Статус заказа изменился на: завершён",
			CreatedAt:  time.Now().Unix(),
			UploadedBy: "system",
		}
		if err := db.Create(&models.ChatMessage{
			OrderID:   chatNotification.OrderID,
			Sender:    chatNotification.Sender,
			Message:   chatNotification.Message,
			CreatedAt: time.Unix(chatNotification.CreatedAt, 0),
		}).Error; err != nil {
			log.Printf("CompleteOrder: Ошибка сохранения уведомления чата для заказа %d: %v", orderID, err)
		}
		websocket.BroadcastMessage(websocket.GetHub(), chatNotification)
		// --- Конец блока уведомления в чат ---

		c.JSON(http.StatusOK, gin.H{"message": "Заказ завершён"})
	}
}

// CancelOrder переводит заказ в статус "отменён". (Администратор)
func CancelOrder(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			OrderID string `form:"orderID" binding:"required"`
		}

		if bindErr := c.ShouldBind(&input); bindErr != nil {
			log.Printf("CancelOrder: Ошибка привязки данных: %v", bindErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверные данные формы: " + bindErr.Error()})
			return
		}

		orderID, parseErr := strconv.ParseUint(input.OrderID, 10, 64)
		if parseErr != nil {
			log.Printf("CancelOrder: Неверный format orderID: %v", parseErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
			return
		}

		var order models.Order
		if err := db.First(&order, uint(orderID)).Error; err != nil {
			log.Printf("CancelOrder: Заказ с ID %d не найден: %v", orderID, err)
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		if err := db.Model(&order).Update("status", "отменён").Error; err != nil {
			log.Printf("CancelOrder: Ошибка обновления статуса заказа %d: %v", orderID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить заказ"})
			return
		}

		log.Printf("Заказ %d переведён в статус 'отменён'", orderID)

		// Отправка уведомления через WebSocket (статус)
		payload := websocket.OrderStatusUpdatePayload{
			OrderID: order.ID,
			Status:  "отменён",
			At:      time.Now().Unix(),
		}
		websocket.BroadcastMessage(websocket.GetHub(), payload)

		// --- Отправка уведомления в чат о смене статуса ---
		chatNotification := websocket.ChatMessagePayload{
			OrderID:    order.ID,
			Sender:     "system",
			Message:    "Статус заказа изменился на: отменён",
			CreatedAt:  time.Now().Unix(),
			UploadedBy: "system",
		}
		if err := db.Create(&models.ChatMessage{
			OrderID:   chatNotification.OrderID,
			Sender:    chatNotification.Sender,
			Message:   chatNotification.Message,
			CreatedAt: time.Unix(chatNotification.CreatedAt, 0),
		}).Error; err != nil {
			log.Printf("CancelOrder: Ошибка сохранения уведомления чата для заказа %d: %v", orderID, err)
		}
		websocket.BroadcastMessage(websocket.GetHub(), chatNotification)
		// --- Конец блока уведомления в чат ---

		c.JSON(http.StatusOK, gin.H{"message": "Заказ отменён"})
	}
}

// DeleteOrder удаляет заказ из базы. (Администратор)
func DeleteOrder(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			OrderID string `form:"orderID" binding:"required"`
		}

		if bindErr := c.ShouldBind(&input); bindErr != nil {
			log.Printf("DeleteOrder: Ошибка привязки данных: %v", bindErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверные данные формы: " + bindErr.Error()})
			return
		}

		orderID, parseErr := strconv.ParseUint(input.OrderID, 10, 64)
		if parseErr != nil {
			log.Printf("DeleteOrder: Неверный format orderID: %v", parseErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
			return
		}

		if err := db.Delete(&models.Order{}, uint(orderID)).Error; err != nil {
			log.Printf("DeleteOrder: Ошибка удаления заказа %d: %v", orderID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось удалить заказ"})
			return
		}

		log.Printf("Заказ %d успешно удалён", orderID)
		c.JSON(http.StatusOK, gin.H{"message": "Заказ удалён"})
	}
}

// AdminEditOrder позволяет администратору редактировать заказ (расширенный вариант).
func AdminEditOrder(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			OrderID            string   `form:"orderID" binding:"required"`
			Topic              string   `form:"topic"`
			Description        string   `form:"description"`
			Deadline           string   `form:"deadline"`
			PlagiarismRequired *bool    `form:"plagiarismRequired"`
			PlagiarismPercent  *uint    `form:"plagiarismPercent"`
			Budget             *float64 `form:"budget"`
			WorkType           string   `form:"workType"`
			Notes              string   `form:"notes"`
			FinalFileURL       string   `form:"finalFileURL"`
		}

		if bindErr := c.ShouldBind(&input); bindErr != nil {
			log.Printf("AdminEditOrder: Ошибка привязки данных: %v", bindErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверные данные формы: " + bindErr.Error()})
			return
		}

		orderID, parseErr := strconv.ParseUint(input.OrderID, 10, 64)
		if parseErr != nil {
			log.Printf("AdminEditOrder: Неверный format orderID: %v", parseErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
			return
		}

		var order models.Order
		if err := db.First(&order, uint(orderID)).Error; err != nil {
			log.Printf("AdminEditOrder: Заказ с ID %d не найден: %v", orderID, err)
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		if input.Deadline != "" {
			var deadline time.Time
			var deadlineParseErr error
			layouts := []string{"2006-01-02", "2006-01-02T15:04:05Z"}
			for _, layout := range layouts {
				deadline, deadlineParseErr = time.Parse(layout, input.Deadline)
				if deadlineParseErr == nil {
					break
				}
			}
			if deadlineParseErr != nil {
				log.Printf("AdminEditOrder: Неверный формат дедлайна для заказа %d: %s", orderID, input.Deadline)
				c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат дедлайна (ожидается YYYY-MM-DD или YYYY-MM-DDTHH:MM:SSZ)"})
				return
			}
			input.Deadline = deadline.Format("2006-01-02")
		}

		if input.PlagiarismRequired != nil && *input.PlagiarismRequired && input.PlagiarismPercent != nil && *input.PlagiarismPercent > 100 {
			log.Printf("AdminEditOrder: Неверное значение PlagiarismPercent для заказа %d: %d", orderID, *input.PlagiarismPercent)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Процент плагиата должен быть не более 100"})
			return
		}

		updates := make(map[string]interface{})
		if input.Topic != "" || c.Request.FormValue("topic") != "" {
			updates["topic"] = input.Topic
		}
		if input.Description != "" || c.Request.FormValue("description") != "" {
			updates["description"] = input.Description
		}
		if input.Deadline != "" || c.Request.FormValue("deadline") != "" {
			updates["deadline"] = input.Deadline
		}
		if input.PlagiarismRequired != nil {
			updates["plagiarism_required"] = *input.PlagiarismRequired
		}
		if input.PlagiarismPercent != nil {
			updates["plagiarism_percent"] = *input.PlagiarismPercent
		}
		if input.Budget != nil {
			updates["budget"] = *input.Budget
		}
		if input.WorkType != "" || c.Request.FormValue("workType") != "" {
			updates["work_type"] = input.WorkType
		}
		if input.Notes != "" || c.Request.FormValue("notes") != "" {
			updates["notes"] = input.Notes
		}
		if input.FinalFileURL != "" || c.Request.FormValue("finalFileURL") != "" {
			updates["final_file_url"] = input.FinalFileURL
		}

		if len(updates) == 0 {
			if err := db.Model(&order).Update("updated_at", time.Now()).Error; err != nil {
				log.Printf("AdminEditOrder: Ошибка обновления времени заказа %d: %v", orderID, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить заказ"})
				return
			}
			log.Printf("Заказ %d успешно отмечен как обновлённый (без изменений полей)", orderID)
			c.JSON(http.StatusOK, gin.H{"message": "Заказ отмечен как обновлённый"})
			return
		}

		if err := db.Model(&order).Updates(updates).Error; err != nil {
			log.Printf("AdminEditOrder: Ошибка обновления заказа %d: %v", orderID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить заказ"})
			return
		}

		log.Printf("Заказ %d успешно обновлён администратором", orderID)
		c.JSON(http.StatusOK, gin.H{"message": "Заказ успешно обновлён администратором"})
	}
}

// ChatHistory возвращает историю сообщений чата для заказа.
func ChatHistory(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		orderIDStr := c.Query("orderID")
		if orderIDStr == "" {
			log.Println("Отсутствует orderID в запросе истории чата")
			c.JSON(http.StatusBadRequest, gin.H{"error": "Отсутствует orderID"})
			return
		}

		orderID, parseErr := strconv.ParseUint(orderIDStr, 10, 64)
		if parseErr != nil {
			log.Printf("ChatHistory: Неверный format orderID: %v", parseErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
			return
		}

		var messages []models.File // Предполагаю, что ты имел в виду файлы как часть чата
		if err := db.Where("order_id = ?", uint(orderID)).Order("created_at asc").Find(&messages).Error; err != nil {
			log.Printf("ChatHistory: Ошибка получения истории чата для OrderID %d: %v", orderID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка получения истории чата"})
			return
		}

		log.Printf("История чата для OrderID %d успешно возвращена (сообщений: %d)", orderID, len(messages))
		c.JSON(http.StatusOK, gin.H{"messages": messages})
	}
}

// GetOrderFiles возвращает список файлов для заказа.
func GetOrderFiles(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		orderIDStr := c.Query("orderID")
		if orderIDStr == "" {
			log.Println("Отсутствует orderID в запросе файлов")
			c.JSON(http.StatusBadRequest, gin.H{"error": "Отсутствует orderID"})
			return
		}

		orderID, parseErr := strconv.ParseUint(orderIDStr, 10, 64)
		if parseErr != nil {
			log.Printf("GetOrderFiles: Неверный формат orderID: %v", parseErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
			return
		}

		var files []models.File
		if err := db.Where("order_id = ?", uint(orderID)).Order("created_at asc").Find(&files).Error; err != nil {
			log.Printf("GetOrderFiles: Ошибка получения файлов для OrderID %d: %v", orderID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка получения файлов"})
			return
		}

		responseFiles := make([]gin.H, len(files))
		for i, file := range files {
			responseFiles[i] = gin.H{
				"filename":     file.OriginalName,
				"originalName": file.OriginalName,
				"uploadedBy":   file.UploadedBy,
				"url":          file.URL,
				"created_at":   file.CreatedAt.Format("2006-01-02 15:04:05"),
			}
		}

		log.Printf("Файлы для OrderID %d успешно возвращены (всего: %d)", orderID, len(files))
		c.JSON(http.StatusOK, gin.H{"files": responseFiles})
	}
}

// GetReview возвращает отзыв для конкретного заказа.
func GetReview(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		orderIDStr := c.Query("orderID")
		if orderIDStr == "" {
			log.Println("Отсутствует orderID в запросе отзыва")
			c.JSON(http.StatusBadRequest, gin.H{"error": "Отсутствует orderID"})
			return
		}

		orderID, parseErr := strconv.ParseUint(orderIDStr, 10, 64)
		if parseErr != nil {
			log.Printf("GetReview: Неверный format orderID: %v", parseErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
			return
		}

		var order models.Order
		if err := db.First(&order, uint(orderID)).Error; err != nil {
			log.Printf("GetReview: Заказ с ID %d не найден: %v", orderID, err)
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		if order.Status != "завершён" {
			log.Printf("GetReview: Нельзя получить отзыв для заказа %d, статус: %s", orderID, order.Status)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Отзыв доступен только для завершённых заказов"})
			return
		}

		var review models.Review
		if err := db.Where("order_id = ?", uint(orderID)).Preload("User").First(&review).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				log.Printf("GetReview: Отзыв для заказа %d не найден", orderID)
				c.JSON(http.StatusOK, gin.H{"review": nil})
				return
			}
			log.Printf("GetReview: Ошибка получения отзыва для заказа %d: %v", orderID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка получения отзыва"})
			return
		}

		response := gin.H{
			"id":         review.ID,
			"order_id":   review.OrderID,
			"user_id":    review.UserID,
			"rating":     review.Rating,
			"comment":    review.Comment,
			"user_name":  review.User.Name,
			"created_at": review.CreatedAt.Format("2006-01-02 15:04:05"),
		}

		log.Printf("Отзыв для заказа %d успешно возвращён", orderID)
		c.JSON(http.StatusOK, gin.H{"review": response})
	}
}

// CreateReview позволяет пользователю оставить отзыв о заказе.
func CreateReview(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			OrderID uint   `form:"orderID" binding:"required"`
			Rating  uint   `form:"rating" binding:"required,gte=1,lte=5"`
			Comment string `form:"comment"`
		}

		if bindErr := c.ShouldBind(&input); bindErr != nil {
			log.Printf("CreateReview: Ошибка привязки данных: %v", bindErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверные данные формы: " + bindErr.Error()})
			return
		}

		var order models.Order
		if err := db.First(&order, input.OrderID).Error; err != nil {
			log.Printf("CreateReview: Заказ с ID %d не найден: %v", input.OrderID, err)
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		if order.Status != "завершён" {
			log.Printf("CreateReview: Нельзя оставить отзыв для заказа %d, статус: %s", input.OrderID, order.Status)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Можно оставить отзыв только для завершённых заказов"})
			return
		}

		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil {
			log.Println("Неавторизованный доступ при создании отзыва")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Необходимо авторизоваться"})
			return
		}

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
				log.Printf("CreateReview: Ошибка преобразования user_id: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Неверный user_id"})
				return
			}
			userID = uint(parsed)
		default:
			log.Println("CreateReview: Неподдерживаемый тип user_id в сессии")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Неверный user_id"})
			return
		}

		if order.UserID != userID {
			log.Printf("Пользователь %d пытается оставить отзыв на чужой заказ %d", userID, input.OrderID)
			c.JSON(http.StatusForbidden, gin.H{"error": "Нет доступа к этому заказу"})
			return
		}

		review := models.Review{
			OrderID: input.OrderID,
			UserID:  userID,
			Rating:  input.Rating,
			Comment: input.Comment,
		}

		if err := db.Create(&review).Error; err != nil {
			log.Printf("CreateReview: Ошибка создания отзыва для заказа %d: %v", input.OrderID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось создать отзыв"})
			return
		}

		log.Printf("Отзыв для заказа %d успешно создан пользователем %d", input.OrderID, userID)
		c.JSON(http.StatusCreated, gin.H{"message": "Отзыв успешно создан", "reviewID": review.ID})
	}
}

// GetReviews возвращает список всех отзывов.
func GetReviews(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var reviews []models.Review
		if err := db.Preload("User").Preload("Order").Find(&reviews).Error; err != nil {
			log.Printf("GetReviews: Ошибка загрузки отзывов: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить отзывы"})
			return
		}

		type ReviewResponse struct {
			ID        uint   `json:"id"`
			Comment   string `json:"comment"`
			Rating    uint   `json:"rating"`
			Author    string `json:"author"`
			CreatedAt string `json:"created_at"`
			WorkType  string `json:"work_type"`
		}

		var response []ReviewResponse
		for _, review := range reviews {
			workType := review.Order.WorkType // Просто берём WorkType из заказа
			if review.Order.ID == 0 {
				log.Printf("Review ID: %d has no associated Order", review.ID)
				workType = "" // Оставляем пустым, фронт сам обработает
			}

			response = append(response, ReviewResponse{
				ID:        review.ID,
				Comment:   review.Comment,
				Rating:    review.Rating,
				Author:    review.User.Name,
				CreatedAt: review.CreatedAt.Format("02.01.2006 15:04"),
				WorkType:  workType,
			})
		}

		c.JSON(http.StatusOK, response)
	}
}

func UpdatePaymentStatus(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			OrderID     string  `form:"orderID" binding:"required"`
			Amount      float64 `form:"amount"`      // Сумма, введённая админом
			PaymentType string  `form:"paymentType"` // "initial" или "additional"
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

		switch input.PaymentType {
		case "initial":
			order.ExtraPayment = input.Amount
		case "additional":
			order.ExtraPayment += input.Amount
		default:
			// Если не указано, пусть будет «additional» или вернём ошибку
			order.ExtraPayment += input.Amount
		}

		// При необходимости отмечаем флаг «IsPaid», если оплачено >= бюджета
		if order.ExtraPayment >= order.Budget {
			order.IsPaid = true
		} else {
			order.IsPaid = false
		}

		if err := db.Save(&order).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить заказ"})
			return
		}

		// Можно отослать уведомление по вебсокету (необязательно)
		payload := websocket.PaymentUpdatePayload{
			Type:         "payment_update", // вот эта строка важна!
			OrderID:      order.ID,
			IsPaid:       order.IsPaid,
			ExtraPayment: order.ExtraPayment,
		}
		websocket.BroadcastMessage(websocket.GetHub(), payload)

		c.JSON(http.StatusOK, gin.H{
			"message": "Оплата обновлена",
			"order": gin.H{
				"id":          order.ID,
				"paid_amount": order.ExtraPayment,
				"budget":      order.Budget,
			},
		})
	}
}

// UpdateLastOnline обновляет время последнего входа администратора.
func UpdateLastOnline(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		orderIDStr := c.PostForm("orderID")
		if orderIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "orderID обязательны"})
			return
		}

		// orderID здесь используется для идентификации, но в данном примере мы обновляем первого администратора
		_, err := strconv.ParseUint(orderIDStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
			return
		}

		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil {
			log.Println("Неавторизованный доступ при удалении файла")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Необходимо авторизоваться"})
			return
		}

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
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Невалидный user_id"})
				return
			}
			userID = uint(parsed)
		default:
			log.Println("Неподдерживаемый тип user_id в сессии")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Невалидный user_id"})
			return
		}

		// Обновляем администратора – здесь выбираем первого user, у которого поле LastLogin обновляем
		var user models.User
		if err := db.Where("id = ?", userID).First(&user).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Администратор не найден"})
			return
		}

		user.LastLogin = time.Now()
		if err := db.Save(&user).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка обновления статуса"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Статус обновлён", "lastOnline": user.LastLogin.Format("02.01.2006 15:04")})
	}
}
