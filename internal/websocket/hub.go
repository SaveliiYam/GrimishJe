package websocket

import (
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

// Глобальная переменная для хаба
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
	hub = h // Инициализируем глобальную переменную
	return h
}

func (h *Hub) Run() {
	ticker := time.NewTicker(30 * time.Second) // Heartbeat для проверки клиентов
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
			log.Printf("Клиент зарегистрирован для OrderID %d (админ: %v, отправитель: %s), общее количество клиентов: %d", client.OrderID, client.IsAdmin, client.UploadedBy, len(h.Clients[client.OrderID]))

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
			log.Printf("Клиент отключён от OrderID %d (админ: %v, отправитель: %s), осталось клиентов: %d", client.OrderID, client.IsAdmin, client.UploadedBy, len(h.Clients[client.OrderID]))

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
							log.Printf("Клиент отключён из-за переполнения канала для OrderID %d (админ: %v, отправитель: %s)", client.OrderID, client.IsAdmin, client.UploadedBy)
						}
					}
				}
			case FileUpdatePayload:
				if clients, ok := h.Clients[payload.OrderID]; ok {
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
			case OrderStatusUpdatePayload:
				if clients, ok := h.Clients[payload.OrderID]; ok {
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

// BroadcastMessage отправляет сообщение через хаб.
func BroadcastMessage(h *Hub, message interface{}) {
	h.Broadcast <- message
}

// GetHub возвращает глобальный хаб.
func GetHub() *Hub {
	return hub
}
