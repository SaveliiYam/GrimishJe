package websocket

import (
	"encoding/json"
	"log"
	"sync"
	"time"

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
			var orderID uint
			var msgBytes []byte
			var err error

			switch payload := message.(type) {
			case ChatMessagePayload:
				orderID = payload.OrderID
				msgBytes, err = json.Marshal(payload)
				if err != nil {
					log.Printf("Ошибка сериализации ChatMessagePayload: %v", err)
					break
				}
			case FileUpdatePayload:
				orderID = payload.OrderID
				msg := map[string]interface{}{
					"type":          "file",
					"order_id":      payload.OrderID,
					"filename":      payload.Filename,
					"original_name": payload.OriginalName,
					"uploaded_by":   payload.UploadedBy,
					"url":           payload.URL,
				}
				msgBytes, err = json.Marshal(msg)
				if err != nil {
					log.Printf("Ошибка сериализации FileUpdatePayload: %v", err)
					break
				}
			case FileDeletePayload:
				orderID = payload.OrderID
				msg := map[string]interface{}{
					"type":     "file_deleted",
					"order_id": payload.OrderID,
					"file_id":  payload.FileID,
				}
				msgBytes, err = json.Marshal(msg)
				if err != nil {
					log.Printf("Ошибка сериализации FileDeletePayload: %v", err)
					break
				}
			case OrderStatusUpdatePayload:
				orderID = payload.OrderID
				msgBytes, err = json.Marshal(payload)
				if err != nil {
					log.Printf("Ошибка сериализации OrderStatusUpdatePayload: %v", err)
					break
				}
			case map[string]interface{}:
				// Обработка случая, когда сообщение уже в формате map
				if oid, ok := payload["order_id"].(float64); ok {
					orderID = uint(oid)
				} else if oid, ok := payload["order_id"].(uint); ok {
					orderID = oid
				} else {
					log.Printf("Не удалось извлечь order_id из сообщения: %v", payload)
					break
				}
				msgBytes, err = json.Marshal(payload)
				if err != nil {
					log.Printf("Ошибка сериализации map[string]interface{}: %v", err)
					break
				}
			default:
				log.Printf("Неизвестный тип сообщения: %v", message)
				break
			}

			if msgBytes != nil {
				if clients, ok := h.Clients[orderID]; ok {
					log.Printf("Отправка сообщения для OrderID %d: %s", orderID, string(msgBytes))
					for client := range clients {
						select {
						case client.Send <- msgBytes:
						default:
							close(client.Send)
							delete(clients, client)
							log.Printf("Клиент отключён из-за переполнения канала для OrderID %d (админ: %v, отправитель: %s)",
								client.OrderID, client.IsAdmin, client.UploadedBy)
						}
					}
				} else {
					log.Printf("Нет клиентов для OrderID %d", orderID)
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
			"type":          "file",
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
	case OrderStatusUpdatePayload:
		hub.Broadcast <- v
	case ChatMessagePayload:
		hub.Broadcast <- v
	default:
		log.Printf("Неизвестный тип сообщения в BroadcastMessage: %v", v)
		hub.Broadcast <- payload
	}
}

func GetHub() *Hub {
	return hub
}
