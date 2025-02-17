package models

import "gorm.io/gorm"

// Order представляет заказ в системе.
type Order struct {
	gorm.Model
	OrderNumber        string  `gorm:"size:100;unique" json:"order_number"` // Индивидуальный номер заказа
	UserID             uint    `json:"user_id"`
	Topic              string  `gorm:"size:255" json:"topic"`
	Description        string  `json:"description"`
	Deadline           string  `json:"deadline"`
	PlagiarismRequired bool    `json:"plagiarism_required"`
	PlagiarismPercent  uint    `json:"plagiarism_percent"`
	Budget             float64 `json:"budget"`
	Status             string  `gorm:"size:50;default:'новый'" json:"status"`
	WorkType           string  `json:"work_type"`
	Notes              string  `json:"notes"`
}
