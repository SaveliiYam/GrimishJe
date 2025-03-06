package websocket

import (
	"log"
	"time"

	"regexp"

	"github.com/MoshKillaPit/GrimishJe/internal/models"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

type Client struct {
	OrderID    uint
	Conn       *websocket.Conn
	Send       chan interface{}
	DB         *gorm.DB
	IsAdmin    bool
	UploadedBy string
}

func (c *Client) readPump() {
	defer func() {
		hub.Unregister <- c
		c.Conn.Close()
		log.Printf("Клиент отключён от чата для OrderID: %d (админ: %v, отправитель: %s)", c.OrderID, c.IsAdmin, c.UploadedBy)
	}()

	c.Conn.SetReadLimit(512 * 1024)
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
		if len(msg.Message) > 1000 {
			log.Printf("Сообщение слишком длинное для OrderID %d, пропущено (админ: %v, отправитель: %s)", c.OrderID, c.IsAdmin, c.UploadedBy)
			continue
		}
		if len(msg.Sender) > 50 || !isValidSender(msg.Sender) {
			log.Printf("Недопустимый sender для OrderID %d: %s (админ: %v, отправитель: %s)", c.OrderID, msg.Sender, c.IsAdmin, c.UploadedBy)
			continue
		}

		log.Printf("Получено сообщение для OrderID %d от %s (админ: %v): %+v", c.OrderID, c.UploadedBy, c.IsAdmin, msg)
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
			log.Printf("Ошибка сохранения сообщения для OrderID %d: %v (админ: %v, отправитель: %s)", c.OrderID, err, c.IsAdmin, c.UploadedBy)
			continue
		}
		if err := c.DB.Where("order_id = ? AND sender = ? AND message = ? AND created_at = ?", chatMsg.OrderID, chatMsg.Sender, chatMsg.Message, chatMsg.CreatedAt).FirstOrCreate(&chatMsg).Error; err != nil {
			log.Printf("Ошибка сохранения или проверки дубликата сообщения для OrderID %d: %v (админ: %v, отправитель: %s)", c.OrderID, err, c.IsAdmin, c.UploadedBy)
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
			log.Printf("Отправлено сообщение/файл/статус для OrderID %d от %s (админ: %v): %+v", c.OrderID, c.UploadedBy, c.IsAdmin, message)
			switch m := message.(type) {
			case ChatMessagePayload:
				if err := c.Conn.WriteJSON(m); err != nil {
					log.Printf("Ошибка записи сообщения для OrderID %d: %v (админ: %v, отправитель: %s)", c.OrderID, err, c.IsAdmin, c.UploadedBy)
					return
				}
			case FileUpdatePayload:
				if err := c.Conn.WriteJSON(m); err != nil {
					log.Printf("Ошибка записи уведомления о файле для OrderID %d: %v (админ: %v, отправитель: %s)", c.OrderID, err, c.IsAdmin, c.UploadedBy)
					return
				}
			case OrderStatusUpdatePayload:
				if err := c.Conn.WriteJSON(m); err != nil {
					log.Printf("Ошибка записи уведомления о статусе для OrderID %d: %v (админ: %v, отправитель: %s)", c.OrderID, err, c.IsAdmin, c.UploadedBy)
					return
				}
			default:
				log.Printf("Неизвестный тип сообщения для OrderID %d: %v", c.OrderID, message)
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

// isValidSender проверяет, допустим ли отправитель.
func isValidSender(sender string) bool {
	return regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(sender)
}
