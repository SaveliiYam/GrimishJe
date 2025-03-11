package websocket

import (
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

var hub *Hub

type Hub struct {
	Clients    map[uint]map[*Client]bool
	Broadcast  chan interface{}
	Register   chan *Client
	Unregister chan *Client
	Mutex      sync.RWMutex
	DB         *gorm.DB
}

func NewHub(db *gorm.DB) *Hub {
	h := &Hub{
		Clients:    make(map[uint]map[*Client]bool),
		Broadcast:  make(chan interface{}, 100),
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
		DB:         db,
	}
	hub = h
	return h
}

func (h *Hub) Run() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case client := <-h.Register:
			h.Mutex.Lock()
			if h.Clients[client.OrderID] == nil {
				h.Clients[client.OrderID] = make(map[*Client]bool)
			}
			h.Clients[client.OrderID][client] = true
			h.Mutex.Unlock()

			log.Printf("Клиент зарегистрирован для OrderID %d (админ: %v, отправитель: %s), общее количество клиентов: %d",
				client.OrderID, client.IsAdmin, client.UploadedBy, len(h.Clients[client.OrderID]))

			// Если клиент админ — отправляем уведомление, что он онлайн, используя тип "executor_status"
			if client.IsAdmin {
				statusPayload := OrderStatusUpdatePayload{
					Type:    "executor_status",
					OrderID: client.OrderID,
					Status:  "Онлайн",
					At:      time.Now().Unix(),
				}
				h.Broadcast <- statusPayload
			}

		case client := <-h.Unregister:
			h.Mutex.Lock()
			if clients, ok := h.Clients[client.OrderID]; ok {
				if _, ok := clients[client]; ok {
					delete(clients, client)
					close(client.Send)
					if len(clients) == 0 {
						delete(h.Clients, client.OrderID)
					}
				}
			}
			h.Mutex.Unlock()
			log.Printf("Клиент отключён от OrderID %d (админ: %v, отправитель: %s), осталось клиентов: %d",
				client.OrderID, client.IsAdmin, client.UploadedBy, len(h.Clients[client.OrderID]))

		case message := <-h.Broadcast:
			h.Mutex.RLock()
			switch payload := message.(type) {
			case ChatMessagePayload:
				if clients, ok := h.Clients[payload.OrderID]; ok {
					for client := range clients {
						select {
						case client.Send <- message:
						default:
							close(client.Send)
							delete(clients, client)
							log.Printf("Клиент отключён из-за переполнения канала для OrderID %d (админ: %v, отправитель: %s)",
								client.OrderID, client.IsAdmin, client.UploadedBy)
						}
					}
				}
			case FileUpdatePayload:
				if clients, ok := h.Clients[payload.OrderID]; ok {
					for client := range clients {
						select {
						case client.Send <- map[string]interface{}{
							"type":          "file", // изменено с "file_update" на "file"
							"order_id":      payload.OrderID,
							"filename":      payload.Filename,
							"original_name": payload.OriginalName,
							"uploaded_by":   payload.UploadedBy,
							"url":           payload.URL,
						}:
						default:
							close(client.Send)
							delete(clients, client)
							log.Printf("Клиент отключён из-за переполнения канала для OrderID %d (админ: %v, отправитель: %s)",
								client.OrderID, client.IsAdmin, client.UploadedBy)
						}
					}
				}
			case OrderStatusUpdatePayload:
				if clients, ok := h.Clients[payload.OrderID]; ok {
					for client := range clients {
						select {
						case client.Send <- message:
						default:
							close(client.Send)
							delete(clients, client)
							log.Printf("Клиент отключён из-за переполнения канала для OrderID %d (админ: %v, отправитель: %s)",
								client.OrderID, client.IsAdmin, client.UploadedBy)
						}
					}
				}
			default:
				log.Printf("Неизвестный тип сообщения: %v", message)
			}
			h.Mutex.RUnlock()

		case <-ticker.C:
			h.Mutex.RLock()
			for orderID, clients := range h.Clients {
				for client := range clients {
					client.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
					if err := client.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
						log.Printf("Ошибка отправки ping для OrderID %d: %v", orderID, err)
						h.Unregister <- client
					}
				}
			}
			h.Mutex.RUnlock()
		}
	}
}

func BroadcastMessage(hub *Hub, payload interface{}) {
	if hub == nil {
		log.Println("WebSocket hub не инициализирован")
		return
	}

	switch v := payload.(type) {
	case FileUpdatePayload:
		hub.Broadcast <- map[string]interface{}{
			"type":          "file", // изменено с "file_update" на "file"
			"order_id":      v.OrderID,
			"filename":      v.Filename,
			"original_name": v.OriginalName,
			"uploaded_by":   v.UploadedBy,
			"url":           v.URL,
		}
	case FileDeletePayload:
		hub.Broadcast <- map[string]interface{}{
			"type":     "file_deleted",
			"order_id": v.OrderID,
			"file_id":  v.FileID,
		}
	default:
		hub.Broadcast <- payload
	}
}

func GetHub() *Hub {
	return hub
}
