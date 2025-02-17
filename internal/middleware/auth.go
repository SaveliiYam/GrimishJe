package middleware

import (
	"log"
	"net/http"

	"github.com/MoshKillaPit/GrimishJe/internal/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AuthRequired проверяет наличие user_id в сессии.
func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		if session.Get("user_id") == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Необходимо авторизоваться"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// Login – обработчик входа, использующий form‑binding и возвращающий JSON с URL для перенаправления.
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
		// Отдаем JSON с URL для перенаправления в зависимости от роли
		if user.IsAdmin {
			c.JSON(http.StatusOK, gin.H{"redirect": "/admin_dashboard"})
		} else {
			c.JSON(http.StatusOK, gin.H{"redirect": "/dashboard"})
		}
	}
}

// Register – обработчик регистрации с form‑binding и редиректом на /dashboard.
func Register(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			Name     string `form:"name" json:"name" binding:"required"`
			Email    string `form:"email" json:"email" binding:"required,email"`
			Password string `form:"password" json:"password" binding:"required,min=6"`
		}
		if err := c.ShouldBind(&input); err != nil {
			log.Printf("Register: Ошибка привязки: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		log.Printf("Register: Получены данные для регистрации: %s", input.Email)
		user := models.User{
			Name:  input.Name,
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

		session := sessions.Default(c)
		session.Set("user_id", user.ID)
		if err := session.Save(); err != nil {
			log.Printf("Register: Ошибка сохранения сессии: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка сохранения сессии"})
			return
		}

		log.Printf("Register: Пользователь успешно зарегистрирован: %s", user.Email)
		// После регистрации редирект для обычных пользователей
		c.Redirect(http.StatusFound, "/dashboard")
	}
}
