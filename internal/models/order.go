package models

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Order представляет заказ в системе.
type Order struct {
	gorm.Model
	OrderNumber        string    `gorm:"type:varchar(100);unique;not null" json:"order_number"`
	UserID             uint      `gorm:"index;not null" json:"user_id"`
	Topic              string    `gorm:"type:varchar(255);not null" json:"topic"`
	Description        string    `gorm:"type:text" json:"description"`
	Deadline           string    `gorm:"type:date;not null" json:"deadline"`
	PlagiarismRequired bool      `gorm:"default:false" json:"plagiarism_required"`
	PlagiarismPercent  uint      `gorm:"check:plagiarism_percent <= 100" json:"plagiarism_percent"`
	Budget             float64   `gorm:"type:decimal(10,2);not null;check:budget > 0" json:"budget"`
	Status             string    `gorm:"type:varchar(50);default:'новый';not null" json:"status"`
	WorkType           string    `gorm:"type:varchar(100);not null" json:"work_type"`
	Notes              string    `gorm:"type:text" json:"notes"`
	CreatedAt          time.Time `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt          time.Time `gorm:"autoUpdateTime" json:"updated_at"`
	CompletedAt        time.Time `gorm:"type:timestamp" json:"completed_at"`
	FinalFileURL       string    `gorm:"type:text" json:"final_file_url" db:"final_file_url"`

	// Новые поля для оплаты:
	IsPaid       bool    `gorm:"default:false" json:"is_paid"`
	ExtraPayment float64 `gorm:"type:decimal(10,2);default:0" json:"extra_payment"`

	// Ассоциации (определяем их один раз):
	User    User     `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`
	Reviews []Review `gorm:"foreignKey:OrderID" json:"reviews"`
}

// ValidStatuses список допустимых статусов заказа.
var ValidStatuses = []string{"новый", "в работе", "завершён", "отменён"}

// BeforeSave хук GORM для проверки допустимого статуса.
func (o *Order) BeforeSave(tx *gorm.DB) error {
	if o.Status != "" {
		statusValid := false
		for _, validStatus := range ValidStatuses {
			if o.Status == validStatus {
				statusValid = true
				break
			}
		}
		if !statusValid {
			return fmt.Errorf("недопустимый статус заказа: %s", o.Status)
		}
	}
	return nil
}

// TableName определяет имя таблицы для модели Order.
func (Order) TableName() string {
	return "orders"
}

// Review представляет отзыв о заказе.
type Review struct {
	gorm.Model
	OrderID uint   `gorm:"index;not null" json:"order_id"`                           // Связь с заказом, индекс для быстрого поиска
	UserID  uint   `gorm:"not null" json:"user_id"`                                  // Связь с пользователем, оставившим отзыв
	Rating  uint   `gorm:"check:rating >= 1 AND rating <= 5;not null" json:"rating"` // Оценка от 1 до 5
	Comment string `gorm:"type:text" json:"comment"`                                 // Комментарий (текстовое поле)
	Order   Order  `gorm:"foreignKey:OrderID;constraint:OnDelete:CASCADE"`           // Связь с заказом, каскадное удаление
	User    User   `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`            // Связь с пользователем, каскадное удаление
}

// TableName определяет имя таблицы для модели Review.
func (Review) TableName() string {
	return "reviews"
}

type File struct {
	gorm.Model
	OrderID      uint      `gorm:"index;not null" json:"order_id"`                  // Связь с заказом, индекс для быстрого поиска
	OriginalName string    `gorm:"type:varchar(255);not null" json:"original_name"` // Оригинальное имя файла
	URL          string    `gorm:"type:text;not null" json:"url"`                   // URL файла в MinIO (увеличиваем до TEXT для поддержки длинных URL)
	UploadedBy   string    `gorm:"type:varchar(50);not null" json:"uploaded_by"`    // Кто загрузил файл (user/admin)
	CreatedAt    time.Time `gorm:"autoCreateTime;not null" json:"created_at"`       // Время загрузки файла
	Order        Order     `gorm:"foreignKey:OrderID;constraint:OnDelete:CASCADE"`  // Связь с заказом, каскадное удаление
}

// TableName определяет имя таблицы для модели File.
func (File) TableName() string {
	return "files"
}
