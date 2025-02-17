package models

import "time"

// ChatMessage представляет сообщение в чате конкретного заказа.
type ChatMessage struct {
	ID        uint      `gorm:"primaryKey"`
	OrderID   uint      `json:"order_id"`
	Sender    string    `json:"sender"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}
