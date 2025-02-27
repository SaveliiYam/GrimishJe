package handlers

import (
	"log"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/MoshKillaPit/GrimishJe/internal/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

// ChatMessagePayload структура для передачи сообщений чата.
type ChatMessagePayload struct {
	OrderID    uint   `json:"order_id"`
	Sender     string `json:"sender"`
	Message    string `json:"message"`
	CreatedAt  int64  `json:"created_at"`
	UploadedBy string `json:"uploaded_by,omitempty"` // Добавлено для идентификации отправителя (user/admin)
}

// Client представляет клиента WebSocket-чата.
type Client struct {
	OrderID    uint
	Conn       *websocket.Conn
	Send       chan ChatMessagePayload
	DB         *gorm.DB
	IsAdmin    bool // Флаг, указывающий, является ли клиент администратором
	UploadedBy string
}

// Hub управляет подключениями клиентов и рассылкой сообщений.
type Hub struct {
	Clients    map[uint]map[*Client]bool
	Broadcast  chan ChatMessagePayload
	Register   chan *Client
	Unregister chan *Client
}

func NewHub() *Hub {
	return &Hub{
		Clients:    make(map[uint]map[*Client]bool),
		Broadcast:  make(chan ChatMessagePayload, 100), // Буфер для предотвращения блокировок
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
	}
}

var hub = NewHub()

// Run запускает цикл обработки событий хаба.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			if h.Clients[client.OrderID] == nil {
				h.Clients[client.OrderID] = make(map[*Client]bool)
			}
			h.Clients[client.OrderID][client] = true
			log.Printf("Клиент зарегистрирован для OrderID %d (админ: %v, отправитель: %s), общее количество клиентов: %d", client.OrderID, client.IsAdmin, client.UploadedBy, len(h.Clients[client.OrderID]))
		case client := <-h.Unregister:
			if clients, ok := h.Clients[client.OrderID]; ok {
				if _, ok := clients[client]; ok {
					delete(clients, client)
					close(client.Send)
					if len(clients) == 0 {
						delete(h.Clients, client.OrderID)
					}
					log.Printf("Клиент отключён от OrderID %d (админ: %v, отправитель: %s), осталось клиентов: %d", client.OrderID, client.IsAdmin, client.UploadedBy, len(clients))
				}
			}
		case message := <-h.Broadcast:
			log.Printf("Broadcast сообщения для OrderID %d от %s (админ: %v): %+v", message.OrderID, message.UploadedBy, message.Sender == "admin", message)
			if clients, ok := h.Clients[message.OrderID]; ok {
				for client := range clients {
					select {
					case client.Send <- message:
					default:
						close(client.Send)
						delete(clients, client)
						log.Printf("Клиент отключён из-за переполнения канала для OrderID %d (админ: %v, отправитель: %s)", client.OrderID, client.IsAdmin, client.UploadedBy)
					}
				}
			}
		}
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return false
		}
		// Разрешить только запросы с вашего домена
		allowedOrigins := []string{"http://localhost:8080", "https://yourdomain.com"}
		for _, allowed := range allowedOrigins {
			if origin == allowed {
				return true
			}
		}
		log.Printf("Запрещённый Origin для WebSocket: %s", origin)
		return false
	},
}

// ChatHandler обрабатывает подключения WebSocket для чата.
func ChatHandler(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Проверка сессии
		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil {
			log.Println("Неавторизованный доступ к WebSocket-чату")
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
		isAdmin = user.IsAdmin // Предполагается, что в модели User есть поле IsAdmin

		// Проверка прав доступа: либо пользователь является владельцем заказа, либо администратором
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

		client := &Client{
			OrderID:    uint(orderID),
			Conn:       ws,
			Send:       make(chan ChatMessagePayload, 100),
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
						UploadedBy: m.Sender, // Устанавливаем uploadedBy как sender для совместимости
					}
					log.Printf("Отправка истории для OrderID %d: %+v", client.OrderID, payload)
					select {
					case client.Send <- payload:
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

// readPump читает сообщения от клиента.
func (c *Client) readPump() {
	defer func() {
		hub.Unregister <- c
		c.Conn.Close()
		log.Printf("Клиент отключён от чата для OrderID: %d (админ: %v, отправитель: %s)", c.OrderID, c.IsAdmin, c.UploadedBy)
	}()

	c.Conn.SetReadLimit(512 * 1024) // Увеличим лимит до 512KB
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		log.Printf("Получен pong для OrderID: %d (админ: %v, отправитель: %s)", c.OrderID, c.IsAdmin, c.UploadedBy)
		return nil
	})

	for {
		var msg ChatMessagePayload
		err := c.Conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket ошибка для OrderID %d (админ: %v, отправитель: %s): %v", c.OrderID, c.IsAdmin, c.UploadedBy, err)
			}
			break
		}

		// Валидация сообщения
		if len(msg.Message) > 1000 { // Ограничим длину сообщения
			log.Printf("Сообщение слишком длинное для OrderID %d, пропущено (админ: %v, отправитель: %s)", c.OrderID, c.IsAdmin, c.UploadedBy)
			continue
		}
		if len(msg.Sender) > 50 || !isValidSender(msg.Sender) { // Проверка отправителя
			log.Printf("Недопустимый sender для OrderID %d: %s (админ: %v, отправитель: %s)", c.OrderID, msg.Sender, c.IsAdmin, c.UploadedBy)
			continue
		}

		log.Printf("Получено сообщение для OrderID %d от %s (админ: %v): %+v", c.OrderID, c.UploadedBy, c.IsAdmin, msg)
		msg.OrderID = c.OrderID
		msg.CreatedAt = time.Now().Unix()
		msg.UploadedBy = c.UploadedBy // Устанавливаем, кто загрузил сообщение (user/admin)

		// Сохраняем сообщение в БД с учётом uploadedBy
		chatMsg := models.ChatMessage{
			OrderID:   c.OrderID,
			Sender:    msg.Sender,
			Message:   msg.Message,
			CreatedAt: time.Unix(msg.CreatedAt, 0),
		}
		if err := c.DB.Create(&chatMsg).Error; err != nil {
			log.Printf("Ошибка сохранения сообщения для OrderID %d: %v (админ: %v, отправитель: %s)", c.OrderID, err, c.IsAdmin, c.UploadedBy)
			continue
		}
		if err := c.DB.Where("order_id = ? AND sender = ? AND message = ? AND created_at = ?", chatMsg.OrderID, chatMsg.Sender, chatMsg.Message, chatMsg.CreatedAt).FirstOrCreate(&chatMsg).Error; err != nil {
			log.Printf("Ошибка сохранения или проверки дубликата сообщения для OrderID %d: %v (админ: %v, отправитель: %s)", c.OrderID, err, c.IsAdmin, c.UploadedBy)
			continue
		}

		hub.Broadcast <- msg
	}
}

// writePump отправляет сообщения клиенту.
func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
		log.Printf("Закрыто соединение для OrderID: %d (админ: %v, отправитель: %s)", c.OrderID, c.IsAdmin, c.UploadedBy)
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			log.Printf("Отправлено сообщение для OrderID %d от %s (админ: %v): %+v", c.OrderID, c.UploadedBy, c.IsAdmin, message)
			if err := c.Conn.WriteJSON(message); err != nil {
				log.Printf("Ошибка записи сообщения для OrderID %d: %v (админ: %v, отправитель: %s)", c.OrderID, err, c.IsAdmin, c.UploadedBy)
				return
			}
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("Ошибка отправки ping для OrderID %d: %v (админ: %v, отправитель: %s)", c.OrderID, err, c.IsAdmin, c.UploadedBy)
				return
			}
			log.Printf("Отправлен ping для OrderID: %d (админ: %v, отправитель: %s)", c.OrderID, c.IsAdmin, c.UploadedBy)
		}
	}
}

// RunChatHub запускает хаб чата в отдельной горутине.
func RunChatHub() {
	go hub.Run()
	log.Println("Чат-хаб запущен")
}

// isValidSender проверяет, допустим ли отправитель (только буквы, цифры, underscores, без специальных символов).
func isValidSender(sender string) bool {
	return regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(sender)
}
