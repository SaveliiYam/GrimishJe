package models

import "gorm.io/gorm"

// Order представляет заказ в системе.
type Order struct {
	gorm.Model
	UserID      uint    `json:"user_id"`
	Title       string  `gorm:"size:255" json:"title"`
	Description string  `json:"description"`
	Price       float64 `json:"price"`
	Status      string  `gorm:"size:50;default:'новый'" json:"status"`
}
