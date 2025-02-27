package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/MoshKillaPit/GrimishJe/internal/models"
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

		// Валидация дедлайна (пробуем парсить различные форматы)
		var deadline time.Time
		var parseErr error
		layouts := []string{
			"2006-01-02",           // Простой формат YYYY-MM-DD
			"2006-01-02T15:04:05Z", // ISO 8601 с Z
		}
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

		// Преобразуем дедлайн в строку формата YYYY-MM-DD
		input.Deadline = deadline.Format("2006-01-02")

		currentTime := time.Now()
		if !deadline.After(currentTime) {
			log.Printf("CreateOrder: Дедлайн (%s) не может быть раньше текущей даты (%s)", input.Deadline, currentTime.Format("2006-01-02"))
			c.JSON(http.StatusBadRequest, gin.H{"error": "Срок выполнения должен быть не раньше сегодняшнего дня"})
			return
		}

		// Ограничение PlagiarismPercent (0-100)
		if input.PlagiarismRequired && input.PlagiarismPercent > 100 {
			log.Printf("CreateOrder: Неверное значение PlagiarismPercent: %d", input.PlagiarismPercent)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Процент плагиата должен быть не более 100"})
			return
		}

		// Извлекаем user_id из сессии
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
			parsed, uidParseErr := strconv.ParseUint(v, 10, 32)
			if uidParseErr != nil {
				log.Printf("CreateOrder: Ошибка преобразования user_id: %v", uidParseErr)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Неверный user_id"})
				return
			}
			userID = uint(parsed)
		default:
			log.Println("CreateOrder: Неподдерживаемый тип user_id в сессии")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Неверный user_id"})
			return
		}

		// Создаём новый заказ
		order := models.Order{
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

		// Генерация OrderNumber (ORD-YYYYMMDD-XXX)
		today := time.Now().Format("20060102")
		var count int64
		if countErr := db.Model(&models.Order{}).
			Where("order_number LIKE ?", "ORD-"+today+"-%").
			Count(&count).Error; countErr != nil {
			log.Printf("CreateOrder: Ошибка подсчёта заказов для генерации номера: %v", countErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при генерации номера заказа"})
			return
		}
		order.OrderNumber = fmt.Sprintf("ORD-%s-%03d", today, count+1)

		// Сохраняем заказ в базе данных
		if createErr := db.Create(&order).Error; createErr != nil {
			log.Printf("CreateOrder: Ошибка создания заказа для пользователя %d: %v", userID, createErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось создать заказ"})
			return
		}

		log.Printf("Создан новый заказ для пользователя %d (OrderID: %d, OrderNumber: %s)", userID, order.ID, order.OrderNumber)
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

		// Извлекаем user_id из сессии
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
			parsed, parseErr := strconv.ParseUint(v, 10, 32)
			if parseErr != nil {
				log.Printf("EditOrder: Ошибка преобразования user_id: %v", parseErr)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Невалидный user_id"})
				return
			}
			userID = uint(parsed)
		default:
			log.Println("EditOrder: Неподдерживаемый тип user_id в сессии")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Невалидный user_id"})
			return
		}

		var order models.Order
		if orderErr := db.First(&order, uint(orderID)).Error; orderErr != nil {
			log.Printf("EditOrder: Заказ с ID %d не найден: %v", orderID, orderErr)
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		// Проверяем, что заказ принадлежит пользователю
		if order.UserID != userID {
			log.Printf("Пользователь %d попытался редактировать чужой заказ %d", userID, orderID)
			c.JSON(http.StatusForbidden, gin.H{"error": "Нет доступа к этому заказу"})
			return
		}

		// Валидация дедлайна (пробуем парсить различные форматы)
		var deadline time.Time
		layouts := []string{
			"2006-01-02",           // Простой формат YYYY-MM-DD
			"2006-01-02T15:04:05Z", // ISO 8601 с Z
		}
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

		// Преобразуем дедлайн в строку формата YYYY-MM-DD
		input.Deadline = deadline.Format("2006-01-02")

		// Обновляем заказ
		order.Deadline = input.Deadline
		order.Notes = input.Notes

		if saveErr := db.Save(&order).Error; saveErr != nil {
			log.Printf("EditOrder: Ошибка обновления заказа %d для пользователя %d: %v", orderID, userID, saveErr)
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
		if orderErr := db.First(&order, uint(orderID)).Error; orderErr != nil {
			log.Printf("AcceptOrder: Заказ с ID %d не найден: %v", orderID, orderErr)
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		// Обновляем статус заказа
		if updateErr := db.Model(&order).Update("status", "в работе").Error; updateErr != nil {
			log.Printf("AcceptOrder: Ошибка обновления статуса заказа %d: %v", orderID, updateErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить заказ"})
			return
		}

		log.Printf("Заказ %d переведён в статус 'в работе'", orderID)
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
		if orderErr := db.First(&order, uint(orderID)).Error; orderErr != nil {
			log.Printf("CompleteOrder: Заказ с ID %d не найден: %v", orderID, orderErr)
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		// Обновляем статус заказа
		if updateErr := db.Model(&order).Update("status", "завершён").Error; updateErr != nil {
			log.Printf("CompleteOrder: Ошибка обновления статуса заказа %d: %v", orderID, updateErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить заказ"})
			return
		}

		log.Printf("Заказ %d переведён в статус 'завершён'", orderID)
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
		if orderErr := db.First(&order, uint(orderID)).Error; orderErr != nil {
			log.Printf("CancelOrder: Заказ с ID %d не найден: %v", orderID, orderErr)
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		// Обновляем статус заказа
		if updateErr := db.Model(&order).Update("status", "отменён").Error; updateErr != nil {
			log.Printf("CancelOrder: Ошибка обновления статуса заказа %d: %v", orderID, updateErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить заказ"})
			return
		}

		log.Printf("Заказ %d переведён в статус 'отменён'", orderID)
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

		if deleteErr := db.Delete(&models.Order{}, uint(orderID)).Error; deleteErr != nil {
			log.Printf("DeleteOrder: Ошибка удаления заказа %d: %v", orderID, deleteErr)
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
		if orderErr := db.First(&order, uint(orderID)).Error; orderErr != nil {
			log.Printf("AdminEditOrder: Заказ с ID %d не найден: %v", orderID, orderErr)
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		// Валидация дедлайна, если он предоставлен
		if input.Deadline != "" {
			var deadline time.Time
			var deadlineParseErr error
			layouts := []string{
				"2006-01-02",           // Простой формат YYYY-MM-DD
				"2006-01-02T15:04:05Z", // ISO 8601 с Z
			}
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
			// Преобразуем дедлайн в строку формата YYYY-MM-DD
			input.Deadline = deadline.Format("2006-01-02")
		}

		// Ограничение PlagiarismPercent, если он предоставлен
		if input.PlagiarismRequired != nil && *input.PlagiarismRequired && input.PlagiarismPercent != nil && *input.PlagiarismPercent > 100 {
			log.Printf("AdminEditOrder: Неверное значение PlagiarismPercent для заказа %d: %d", orderID, *input.PlagiarismPercent)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Процент плагиата должен быть не более 100"})
			return
		}

		// Обновляем только те поля, которые были переданы (даже пустые значения, если они отправлены)
		updates := make(map[string]interface{})
		if input.Topic != "" || c.Request.FormValue("topic") != "" { // Проверяем FormValue, если поле пустое
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

		// Если не переданы поля для обновления, обновляем только время (UpdatedAt)
		if len(updates) == 0 {
			if updateErr := db.Model(&order).Update("updated_at", time.Now()).Error; updateErr != nil {
				log.Printf("AdminEditOrder: Ошибка обновления времени заказа %d: %v", orderID, updateErr)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить заказ"})
				return
			}
			log.Printf("Заказ %d успешно отмечен как обновлённый (без изменений полей)", orderID)
			c.JSON(http.StatusOK, gin.H{"message": "Заказ отмечен как обновлённый"})
			return
		}

		// Обновляем только указанные поля, сохраняя OrderNumber и другие неизменяемые поля
		if updateErr := db.Model(&order).Updates(updates).Error; updateErr != nil {
			log.Printf("AdminEditOrder: Ошибка обновления заказа %d: %v", orderID, updateErr)
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

		var messages []models.ChatMessage
		if msgErr := db.Where("order_id = ?", uint(orderID)).Order("created_at asc").Find(&messages).Error; msgErr != nil {
			log.Printf("ChatHistory: Ошибка получения истории чата для OrderID %d: %v", orderID, msgErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка получения истории чата"})
			return
		}

		log.Printf("История чата для OrderID %d успешно возвращена (сообщений: %d)", orderID, len(messages))
		c.JSON(http.StatusOK, gin.H{"messages": messages})
	}
}

// GetAdminOrder возвращает детали конкретного заказа для админ-панели.
func GetAdminOrder(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		orderIDStr := c.Param("id")
		orderID, parseErr := strconv.ParseUint(orderIDStr, 10, 64)
		if parseErr != nil {
			log.Printf("GetAdminOrder: Неверный format orderID: %v", parseErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
			return
		}

		var order models.Order
		if orderErr := db.Preload("User").First(&order, uint(orderID)).Error; orderErr != nil {
			log.Printf("GetAdminOrder: Заказ с ID %d не найден: %v", orderID, orderErr)
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		responseOrder := gin.H{
			"ID":                 order.ID,
			"OrderNumber":        order.OrderNumber,
			"User":               gin.H{"Name": order.User.Name, "Phone": order.User.Phone, "Telegram": order.User.Telegram},
			"Topic":              order.Topic,
			"Deadline":           order.Deadline, // Оставляем как есть, GORM должен вернуть строку в формате YYYY-MM-DD
			"PlagiarismRequired": order.PlagiarismRequired,
			"PlagiarismPercent":  order.PlagiarismPercent,
			"Budget":             order.Budget,
			"Status":             order.Status,
			"WorkType":           order.WorkType,
			"Notes":              order.Notes,
			"CreatedAt":          order.CreatedAt.Format("2006-01-02"),
		}

		log.Printf("Детали заказа %d успешно возвращены", orderID)
		c.JSON(http.StatusOK, gin.H{"order": responseOrder})
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
		if orderErr := db.First(&order, uint(orderID)).Error; orderErr != nil {
			log.Printf("GetReview: Заказ с ID %d не найден: %v", orderID, orderErr)
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		// Проверяем, что заказ завершён
		if order.Status != "завершён" {
			log.Printf("GetReview: Нельзя получить отзыв для заказа %d, статус: %s", orderID, order.Status)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Отзыв доступен только для завершённых заказов"})
			return
		}

		var review models.Review
		if reviewErr := db.Where("order_id = ?", uint(orderID)).Preload("User").First(&review).Error; reviewErr != nil {
			if reviewErr == gorm.ErrRecordNotFound {
				log.Printf("GetReview: Отзыв для заказа %d не найден", orderID)
				c.JSON(http.StatusOK, gin.H{"review": nil})
				return
			}
			log.Printf("GetReview: Ошибка получения отзыва для заказа %d: %v", orderID, reviewErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка получения отзыва"})
			return
		}

		// Форматируем ответ
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

		// Получаем заказ, чтобы проверить его статус
		var order models.Order
		if orderErr := db.First(&order, input.OrderID).Error; orderErr != nil {
			log.Printf("CreateReview: Заказ с ID %d не найден: %v", input.OrderID, orderErr)
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		// Проверяем, что заказ завершён
		if order.Status != "завершён" {
			log.Printf("CreateReview: Нельзя оставить отзыв для заказа %d, статус: %s", input.OrderID, order.Status)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Можно оставить отзыв только для завершённых заказов"})
			return
		}

		// Получаем user_id из сессии
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
		default:
			log.Println("CreateReview: Неподдерживаемый тип user_id в сессии")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Неверный user_id"})
			return
		}

		// Проверяем, что пользователь является владельцем заказа
		if order.UserID != userID {
			log.Printf("Пользователь %d пытается оставить отзыв на чужой заказ %d", userID, input.OrderID)
			c.JSON(http.StatusForbidden, gin.H{"error": "Нет доступа к этому заказу"})
			return
		}

		// Создаём новый отзыв
		review := models.Review{
			OrderID: input.OrderID,
			UserID:  userID,
			Rating:  input.Rating,
			Comment: input.Comment,
		}

		if createErr := db.Create(&review).Error; createErr != nil {
			log.Printf("CreateReview: Ошибка создания отзыва для заказа %d: %v", input.OrderID, createErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось создать отзыв"})
			return
		}

		log.Printf("Отзыв для заказа %d успешно создан пользователем %d", input.OrderID, userID)
		c.JSON(http.StatusCreated, gin.H{"message": "Отзыв успешно создан", "reviewID": review.ID})
	}
}
