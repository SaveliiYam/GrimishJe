package websocket

import (
	"log"
	"regexp"
	"time"

	"github.com/MoshKillaPit/GrimishJe/internal/models"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

type Client struct {
	OrderID    uint
	UserID     uint
	Conn       *websocket.Conn
	Send       chan []byte // Убедимся, что это chan []byte
	DB         *gorm.DB
	IsAdmin    bool
	UploadedBy string
}

func (c *Client) readPump() {
	defer func() {
		if c.IsAdmin {
			c.DB.Model(&models.User{}).
				Where("id = ?", c.UserID).
				Update("last_login", time.Now())

			offlinePayload := OrderStatusUpdatePayload{
				Type:    "executor_status",
				OrderID: c.OrderID,
				Status:  "Оффлайн (был в сети: " + time.Now().Format("02.01.2006 15:04") + ")",
				At:      time.Now().Unix(),
			}
			BroadcastMessage(GetHub(), offlinePayload)
		}

		hub.Unregister <- c
		c.Conn.Close()
		log.Printf("Клиент отключён от чата для OrderID: %d (админ: %v, отправитель: %s)",
			c.OrderID, c.IsAdmin, c.UploadedBy)
	}()

	c.Conn.SetReadLimit(512 * 1024)
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		log.Printf("Получен pong для OrderID: %d (админ: %v, отправитель: %s)",
			c.OrderID, c.IsAdmin, c.UploadedBy)
		return nil
	})

	for {
		var msg ChatMessagePayload
		err := c.Conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket ошибка для OrderID %d (админ: %v, отправитель: %s): %v",
					c.OrderID, c.IsAdmin, c.UploadedBy, err)
			}
			break
		}

		if len(msg.Message) > 1000 {
			log.Printf("Сообщение слишком длинное для OrderID %d, пропущено (админ: %v, отправитель: %s)",
				c.OrderID, c.IsAdmin, c.UploadedBy)
			continue
		}
		if len(msg.Sender) > 50 || !isValidSender(msg.Sender) {
			log.Printf("Недопустимый sender для OrderID %d: %s (админ: %v, отправитель: %s)",
				c.OrderID, msg.Sender, c.IsAdmin, c.UploadedBy)
			continue
		}

		log.Printf("Получено сообщение для OrderID %d от %s (админ: %v): %+v",
			c.OrderID, c.UploadedBy, c.IsAdmin, msg)
		msg.OrderID = c.OrderID
		msg.CreatedAt = time.Now().Unix()
		msg.UploadedBy = c.UploadedBy

		chatMsg := models.ChatMessage{
			OrderID:   c.OrderID,
			Sender:    msg.Sender,
			Message:   msg.Message,
			CreatedAt: time.Unix(msg.CreatedAt, 0),
		}
		if err := c.DB.Create(&chatMsg).Error; err != nil {
			log.Printf("Ошибка сохранения сообщения для OrderID %d: %v (админ: %v, отправитель: %s)",
				c.OrderID, err, c.IsAdmin, c.UploadedBy)
			continue
		}
		if err := c.DB.Where("order_id = ? AND sender = ? AND message = ? AND created_at = ?",
			chatMsg.OrderID, chatMsg.Sender, chatMsg.Message, chatMsg.CreatedAt).
			FirstOrCreate(&chatMsg).Error; err != nil {
			log.Printf("Ошибка проверки дубликата сообщения для OrderID %d: %v (админ: %v, отправитель: %s)",
				c.OrderID, err, c.IsAdmin, c.UploadedBy)
			continue
		}

		BroadcastMessage(hub, msg)
	}
}

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
			log.Printf("Отправлено сообщение/файл/статус для OrderID %d от %s (админ: %v): %s",
				c.OrderID, c.UploadedBy, c.IsAdmin, string(message))
			if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("Ошибка записи сообщения для OrderID %d: %v (админ: %v, отправитель: %s)",
					c.OrderID, err, c.IsAdmin, c.UploadedBy)
				return
			}
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("Ошибка отправки ping для OrderID %d: %v (админ: %v, отправитель: %s)",
					c.OrderID, err, c.IsAdmin, c.UploadedBy)
				return
			}
			log.Printf("Отправлен ping для OrderID: %d (админ: %v, отправитель: %s)", c.OrderID, c.IsAdmin, c.UploadedBy)
		}
	}
}

func isValidSender(sender string) bool {
	// Разрешаем латиницу, кириллицу, цифры и подчёркивания
	return regexp.MustCompile(`^[a-zA-Z0-9_\p{Cyrillic}]+$`).MatchString(sender)
}
