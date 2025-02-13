package models

import (
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	// Указываем имя индекса, совпадающее с тем, что создал PostgreSQL
	Email    string `gorm:"uniqueIndex:users_email_key;size:255" json:"email"`
	Password string `json:"password"`
	// Дополнительно можно добавить: Name, Phone, Telegram и т.д.
}

// SetPassword хэширует пароль и сохраняет его.
func (u *User) SetPassword(password string) error {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.Password = string(hashed)
	return nil
}

// CheckPassword сравнивает введённый пароль с сохранённым хэшем.
func (u *User) CheckPassword(password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password)) == nil
}
