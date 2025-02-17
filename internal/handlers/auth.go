package handlers

import (
	"log"
	"net/http"

	"github.com/MoshKillaPit/GrimishJe/internal/models"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Register обрабатывает регистрацию пользователя, сохраняет сессию и отправляет ответ.
func Register(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			Name     string `json:"name" binding:"required"` // Добавлено поле Name
			Email    string `json:"email" binding:"required,email"`
			Password string `json:"password" binding:"required,min=6"`
		}

		if err := c.ShouldBindJSON(&input); err != nil {
			log.Printf("Register: Ошибка привязки JSON: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		log.Printf("Register: Получены данные для регистрации: %s", input.Email)
		user := models.User{
			Name:  input.Name, // Сохраняем имя пользователя
			Email: input.Email,
		}
		if err := user.SetPassword(input.Password); err != nil {
			log.Printf("Register: Ошибка установки пароля: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка установки пароля"})
			return
		}
		if err := db.Create(&user).Error; err != nil {
			log.Printf("Register: Ошибка создания пользователя: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Пользователь с таким email уже существует"})
			return
		}

		// Сохраняем сессию
		session := sessions.Default(c)
		session.Set("user_id", user.ID)
		if err := session.Save(); err != nil {
			log.Printf("Register: Ошибка сохранения сессии: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка сохранения сессии"})
			return
		}

		log.Printf("Register: Пользователь успешно зарегистрирован: %s", user.Email)
		c.JSON(http.StatusCreated, gin.H{"message": "Регистрация прошла успешно"})
	}
}

// Login обрабатывает вход пользователя, сохраняет сессию и отправляет ответ.
func Login(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			Email    string `form:"email" binding:"required,email"`
			Password string `form:"password" binding:"required"`
		}
		if err := c.ShouldBind(&input); err != nil {
			log.Printf("Login: Ошибка привязки: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var user models.User
		if err := db.Where("email = ?", input.Email).First(&user).Error; err != nil {
			log.Printf("Login: Пользователь не найден: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Неверный email или пароль"})
			return
		}

		if !user.CheckPassword(input.Password) {
			log.Printf("Login: Неверный пароль для %s", input.Email)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Неверный email или пароль"})
			return
		}

		session := sessions.Default(c)
		session.Set("user_id", user.ID)
		if err := session.Save(); err != nil {
			log.Printf("Login: Ошибка сохранения сессии: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка сохранения сессии"})
			return
		}

		log.Printf("Login: Пользователь успешно вошел: %s", user.Email)
		// Перенаправляем в зависимости от роли
		if user.IsAdmin {
			c.Redirect(http.StatusFound, "/admin_dashboard")
		} else {
			c.Redirect(http.StatusFound, "/dashboard")
		}
	}
}
