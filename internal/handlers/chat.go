package handlers

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/MoshKillaPit/GrimishJe/internal/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

type ChatMessagePayload struct {
	OrderID   uint   `json:"order_id"`
	Sender    string `json:"sender"`
	Message   string `json:"message"`
	CreatedAt int64  `json:"created_at"`
}

type Client struct {
	OrderID uint
	Conn    *websocket.Conn
	Send    chan ChatMessagePayload
	DB      *gorm.DB
}

type Hub struct {
	Clients    map[uint]map[*Client]bool
	Broadcast  chan ChatMessagePayload
	Register   chan *Client
	Unregister chan *Client
}

func NewHub() *Hub {
	return &Hub{
		Clients:    make(map[uint]map[*Client]bool),
		Broadcast:  make(chan ChatMessagePayload),
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
	}
}

var hub = NewHub()

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			if h.Clients[client.OrderID] == nil {
				h.Clients[client.OrderID] = make(map[*Client]bool)
			}
			h.Clients[client.OrderID][client] = true
		case client := <-h.Unregister:
			if clients, ok := h.Clients[client.OrderID]; ok {
				if _, ok := clients[client]; ok {
					delete(clients, client)
					close(client.Send)
					if len(clients) == 0 {
						delete(h.Clients, client.OrderID)
					}
				}
			}
		case message := <-h.Broadcast:
			log.Printf("Broadcast для OrderID %d: %+v", message.OrderID, message) // Логирование отправляемых данных
			if clients, ok := h.Clients[message.OrderID]; ok {
				for client := range clients {
					select {
					case client.Send <- message:
					default:
						close(client.Send)
						delete(clients, client)
					}
				}
			}
		}
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func ChatHandler(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Проверка сессии
		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Не авторизован"})
			return
		}

		orderIDStr := c.Query("orderID")
		orderID, err := strconv.ParseUint(orderIDStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid orderID"})
			return
		}

		// Проверка, принадлежит ли заказ пользователю
		var order models.Order
		if err := db.First(&order, uint(orderID)).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
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
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Невалидный user_id"})
				return
			}
			userID = uint(parsed)
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Невалидный user_id"})
			return
		}

		// Проверяем, что пользователь имеет доступ к заказу
		if order.UserID != userID {
			c.JSON(http.StatusForbidden, gin.H{"error": "Нет доступа к этому заказу"})
			return
		}

		ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Println("WebSocket upgrade error:", err)
			return
		}
		client := &Client{
			OrderID: uint(orderID),
			Conn:    ws,
			Send:    make(chan ChatMessagePayload),
			DB:      db,
		}
		hub.Register <- client

		// Загрузка истории сообщений для данного заказа
		var messages []models.ChatMessage
		if err := db.Where("order_id = ?", client.OrderID).Order("created_at asc").Find(&messages).Error; err != nil {
			log.Println("Ошибка загрузки истории чата:", err)
		} else {
			go func() {
				for _, m := range messages {
					payload := ChatMessagePayload{
						OrderID:   m.OrderID,
						Sender:    m.Sender,
						Message:   m.Message,
						CreatedAt: m.CreatedAt.Unix(), // Убедимся, что всегда отправляем Unix-время
					}
					log.Printf("Отправка истории для OrderID %d: %+v", client.OrderID, payload) // Логирование
					client.Send <- payload
				}
			}()
		}

		go client.readPump()
		go client.writePump()
	}
}

func (c *Client) readPump() {
	defer func() {
		hub.Unregister <- c
		c.Conn.Close()
		log.Println("Клиент отключён от чата для OrderID:", c.OrderID)
	}()
	c.Conn.SetReadLimit(512)
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		log.Println("Получен pong для OrderID:", c.OrderID)
		return nil
	})
	for {
		var msg ChatMessagePayload
		err := c.Conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket ошибка для OrderID %d: %v", c.OrderID, err)
			}
			break
		}
		log.Printf("Получено сообщение для OrderID %d: %+v", c.OrderID, msg)
		msg.OrderID = c.OrderID
		msg.CreatedAt = time.Now().Unix()

		// Сохраняем сообщение в БД
		chatMsg := models.ChatMessage{
			OrderID:   c.OrderID,
			Sender:    msg.Sender,
			Message:   msg.Message,
			CreatedAt: time.Unix(msg.CreatedAt, 0),
		}
		if err := c.DB.Create(&chatMsg).Error; err != nil {
			log.Println("Ошибка сохранения сообщения для OrderID", c.OrderID, ":", err)
		}

		hub.Broadcast <- msg
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
		log.Println("Закрыто соединение для OrderID:", c.OrderID)
	}()
	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			log.Printf("Отправлено сообщение для OrderID %d: %+v", c.OrderID, message)
			if err := c.Conn.WriteJSON(message); err != nil {
				log.Println("Ошибка записи сообщения для OrderID", c.OrderID, ":", err)
				return
			}
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Println("Ошибка отправки ping для OrderID", c.OrderID, ":", err)
				return
			}
			log.Println("Отправлен ping для OrderID:", c.OrderID)
		}
	}
}

func RunChatHub() {
	go hub.Run()
}
