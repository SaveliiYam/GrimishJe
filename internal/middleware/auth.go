package middleware

import (
	"log"
	"net/http"

	"github.com/MoshKillaPit/GrimishJe/internal/models"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Register обрабатывает регистрацию пользователя, сохраняет сессию и перенаправляет на /dashboard.
func Register(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			Email    string `json:"email" binding:"required,email"`
			Password string `json:"password" binding:"required,min=6"`
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

		session := sessions.Default(c)
		session.Set("user_id", user.ID)
		if err := session.Save(); err != nil {
			log.Printf("Register: Ошибка сохранения сессии: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка сохранения сессии"})
			return
		}

		log.Printf("Register: Пользователь успешно зарегистрирован: %s", user.Email)
		// Перенаправление на /dashboard только при успешной регистрации
		c.Redirect(http.StatusFound, "/dashboard")
	}
}

func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")
		if userID == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Необходимо авторизоваться"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// Login обрабатывает вход пользователя, сохраняет сессию и перенаправляет на /dashboard.
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

		session := sessions.Default(c)
		session.Set("user_id", user.ID)
		if err := session.Save(); err != nil {
			log.Printf("Login: Ошибка сохранения сессии: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка сохранения сессии"})
			return
		}

		log.Printf("Login: Пользователь успешно вошел: %s", user.Email)
		// Перенаправление на /dashboard только при успешном входе
		c.Redirect(http.StatusFound, "/dashboard")
	}
}

// Dashboard – пример защищённого обработчика, который доступен только авторизованным пользователям.
func Dashboard() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "Добро пожаловать на защищенную страницу!"})
	}
}
