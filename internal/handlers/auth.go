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
			Email    string `json:"email" binding:"required,email"`
			Password string `json:"password" binding:"required,min=6"`
			// Можно добавить и другие поля, например имя, телефон и т.д.
		}

		if err := c.ShouldBindJSON(&input); err != nil {
			log.Printf("Register: Ошибка привязки JSON: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		log.Printf("Register: Получены данные для регистрации: %s", input.Email)
		user := models.User{Email: input.Email}
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
		// Можно сделать редирект на /dashboard
		// c.Redirect(http.StatusFound, "/dashboard")
		c.JSON(http.StatusCreated, gin.H{"message": "Регистрация прошла успешно"})
	}
}

// Login обрабатывает вход пользователя, сохраняет сессию и отправляет ответ.
func Login(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			Email    string `json:"email" binding:"required,email"`
			Password string `json:"password" binding:"required"`
		}

		if err := c.ShouldBindJSON(&input); err != nil {
			log.Printf("Login: Ошибка привязки JSON: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		log.Printf("Login: Получены данные для входа: %s", input.Email)
		var user models.User
		if err := db.Where("email = ?", input.Email).First(&user).Error; err != nil {
			log.Printf("Login: Пользователь не найден: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Неверный email или пароль"})
			return
		}
		if !user.CheckPassword(input.Password) {
			log.Printf("Login: Неверный пароль для email: %s", input.Email)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Неверный email или пароль"})
			return
		}

		// Сохраняем сессию
		session := sessions.Default(c)
		session.Set("user_id", user.ID)
		if err := session.Save(); err != nil {
			log.Printf("Login: Ошибка сохранения сессии: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка сохранения сессии"})
			return
		}

		log.Printf("Login: Пользователь успешно вошел: %s", user.Email)
		// Можно сделать редирект на /dashboard
		// c.Redirect(http.StatusFound, "/dashboard")
		c.JSON(http.StatusOK, gin.H{"message": "Вход выполнен успешно"})
	}
}
