package models

import (
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	Email    string `gorm:"uniqueIndex:users_email_key;size:255" json:"email"`
	Password string `json:"password" binding:"required,min=6"`
	Name     string `json:"name" binding:"required"`
	Phone    string `gorm:"size:20" json:"phone"`          // Новое поле для номера телефона
	Telegram string `gorm:"size:50" json:"telegram"`       // Новое поле для Telegram
	IsAdmin  bool   `gorm:"default:false" json:"is_admin"` // true, если пользователь — администратор
}

func (u *User) SetPassword(password string) error {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.Password = string(hashed)
	return nil
}

func (u *User) CheckPassword(password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password)) == nil
}
