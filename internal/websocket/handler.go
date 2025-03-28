package websocket

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/MoshKillaPit/GrimishJe/internal/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return false
		}
		allowedOrigins := []string{"http://localhost:8080", "https://gromish.ru"}
		for _, allowed := range allowedOrigins {
			if origin == allowed {
				return true
			}
		}
		log.Printf("Запрещённый Origin для WebSocket: %s", origin)
		return false
	},
}

// WebSocketHandler обрабатывает подключения WebSocket для чата и уведомлений.
func WebSocketHandler(hub *Hub) gin.HandlerFunc {
	return func(c *gin.Context) {
		db, _ := c.MustGet("db").(*gorm.DB)

		// Проверка сессии
		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil {
			log.Println("Неавторизованный доступ к WebSocket")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Не авторизован"})
			return
		}

		orderIDStr := c.Query("orderID")
		if orderIDStr == "" {
			log.Println("Отсутствует orderID в запросе WebSocket")
			c.JSON(http.StatusBadRequest, gin.H{"error": "Отсутствует orderID"})
			return
		}

		orderID, err := strconv.ParseUint(orderIDStr, 10, 64)
		if err != nil {
			log.Printf("Неверный формат orderID: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат orderID"})
			return
		}

		// Проверка заказа
		var order models.Order
		if err := db.First(&order, uint(orderID)).Error; err != nil {
			log.Printf("Заказ с ID %d не найден: %v", orderID, err)
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
			return
		}

		var userID uint
		var isAdmin bool
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

		// Проверяем, является ли пользователь администратором
		var user models.User
		if err := db.First(&user, userID).Error; err != nil {
			log.Printf("Пользователь с ID %d не найден: %v", userID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Пользователь не найден"})
			return
		}
		isAdmin = user.IsAdmin

		// Проверка прав доступа
		uploadedBy := "user"
		if isAdmin {
			uploadedBy = "admin"
		} else if order.UserID != userID {
			log.Printf("Пользователь %d (не администратор) попытался получить доступ к чужому заказу %d", userID, orderID)
			c.JSON(http.StatusForbidden, gin.H{"error": "Нет доступа к этому заказу"})
			return
		}

		ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Printf("Ошибка обновления WebSocket для OrderID %d: %v", orderID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка WebSocket соединения"})
			return
		}

		// Создаем клиента с каналом Send как chan []byte
		client := &Client{
			OrderID:    uint(orderID),
			UserID:     user.ID,
			Conn:       ws,
			Send:       make(chan []byte, 100), // Используем chan []byte
			DB:         db,
			IsAdmin:    isAdmin,
			UploadedBy: uploadedBy,
		}
		hub.Register <- client

		// Загрузка истории сообщений для данного заказа
		var messages []models.ChatMessage
		if err := db.Where("order_id = ?", client.OrderID).Order("created_at asc").Find(&messages).Error; err != nil {
			log.Printf("Ошибка загрузки истории чата для OrderID %d: %v", client.OrderID, err)
		} else {
			go func() {
				for _, m := range messages {
					payload := ChatMessagePayload{
						OrderID:    m.OrderID,
						Sender:     m.Sender,
						Message:    m.Message,
						CreatedAt:  m.CreatedAt.Unix(),
						UploadedBy: m.Sender,
					}
					log.Printf("Отправка истории для OrderID %d: %+v", client.OrderID, payload)
					jsonData, err := json.Marshal(payload)
					if err != nil {
						log.Printf("Ошибка сериализации истории для OrderID %d: %v", client.OrderID, err)
						continue
					}
					select {
					case client.Send <- jsonData:
					default:
						log.Printf("Переполнение канала для отправки истории OrderID %d", client.OrderID)
					}
				}
			}()
		}

		go client.readPump()
		go client.writePump()
	}
}
