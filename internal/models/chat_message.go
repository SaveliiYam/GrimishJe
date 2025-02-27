package models

import (
	"time"
)

// ChatMessage представляет сообщение в чате конкретного заказа.
type ChatMessage struct {
	ID         uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	OrderID    uint      `gorm:"index;not null" json:"order_id"`            // Связь с заказом, индекс для быстрого поиска
	UserID     uint      `gorm:"not null" json:"user_id"`                   // Связь с пользователем, отправившим сообщение
	Sender     string    `gorm:"type:varchar(50);not null" json:"sender"`   // Отправитель (максимум 50 символов)
	Message    string    `gorm:"type:text;not null" json:"message"`         // Текст сообщения (текстовое поле)
	CreatedAt  time.Time `gorm:"autoCreateTime;not null" json:"created_at"` // Время создания сообщения
	UpdatedAt  time.Time `gorm:"autoUpdateTime" json:"updated_at"`          // Время последнего обновления (опционально)
	IsRead     bool      `gorm:"default:false" json:"is_read"`
	UploadedBy string    `gorm:"type:varchar(10)"` // Например, "user" или "admin"           // Прочитано ли сообщение
}

// TableName определяет имя таблицы для модели ChatMessage.
func (ChatMessage) TableName() string {
	return "chat_messages"
}
