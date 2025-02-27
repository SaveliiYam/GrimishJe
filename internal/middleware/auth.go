package middleware

import (
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/MoshKillaPit/GrimishJe/internal/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AuthRequired проверяет наличие user_id в сессии.
func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil {
			log.Println("Неавторизованный доступ к защищённому ресурсу")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Необходимо авторизоваться"})
			c.Abort()
			return
		}

		// Преобразуем user_id в uint для проверки
		var userID uint
		switch v := uid.(type) {
		case uint:
			userID = v
		case int:
			userID = uint(v)
		case int64:
			userID = uint(v)
		case string:
			parsed, err := strconv.ParseUint(v, 10, 32)
			if err != nil {
				log.Printf("AuthRequired: Ошибка преобразования user_id: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Неверный формат user_id"})
				c.Abort()
				return
			}
			userID = uint(parsed)
		default:
			log.Println("AuthRequired: Неподдерживаемый тип user_id в сессии")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Неверный формат user_id"})
			c.Abort()
			return
		}

		log.Printf("Пользователь с ID %d прошёл проверку авторизации, сессия: %+v", userID, session)
		c.Next()
	}
}

// Login – обработчик входа, использующий form-binding и возвращающий JSON с URL для перенаправления.
func Login(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			Email    string `form:"email" binding:"required,email"`
			Password string `form:"password" binding:"required"`
		}

		if err := c.ShouldBind(&input); err != nil {
			log.Printf("Login: Ошибка привязки данных: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверные данные формы: " + err.Error()})
			return
		}

		// Нормализуем email
		normalizedEmail := strings.ToLower(input.Email)

		var user models.User
		if err := db.Where("email = ?", normalizedEmail).First(&user).Error; err != nil {
			log.Printf("Login: Пользователь с email %s не найден: %v", normalizedEmail, err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Неверный email или пароль"})
			return
		}

		if !user.CheckPassword(input.Password) {
			log.Printf("Login: Неверный пароль для пользователя %s", user.Email)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Неверный email или пароль"})
			return
		}

		session := sessions.Default(c)
		session.Set("user_id", user.ID)
		if err := session.Save(); err != nil {
			log.Printf("Login: Ошибка сохранения сессии для пользователя %s: %v", user.Email, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка сохранения сессии"})
			return
		}

		log.Printf("Login: Пользователь с email %s (ID: %d) успешно вошел, сессия: %+v", user.Email, user.ID, session)
		if user.IsAdmin {
			c.JSON(http.StatusOK, gin.H{
				"message":  "Вход выполнен успешно",
				"redirect": "/admin_dashboard",
			})
		} else {
			c.JSON(http.StatusOK, gin.H{
				"message":  "Вход выполнен успешно",
				"redirect": "/dashboard",
			})
		}
	}
}

// Register – обработчик регистрации с form-binding и возвращением JSON для перенаправления.
func Register(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			Name     string `form:"name" binding:"required"`
			Email    string `form:"email" binding:"required,email"`
			Password string `form:"password" binding:"required,min=6"`
			Phone    string `form:"phone" binding:"required"`
			Telegram string `form:"telegram" binding:"required"`
		}

		if err := c.ShouldBind(&input); err != nil {
			log.Printf("Register: Ошибка привязки данных: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверные данные формы: " + err.Error()})
			return
		}

		// Нормализация и валидация телефона
		normalizedPhone := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(input.Phone, " ", ""), "(", ""), ")", "")
		normalizedPhone = strings.ReplaceAll(normalizedPhone, "-", "")
		phoneRegex := regexp.MustCompile(`^\+?7\d{10}$|^7\d{10}$`) // Разрешён формат +7XXXXXXXXXX или 7XXXXXXXXXX
		if !phoneRegex.MatchString(normalizedPhone) {
			log.Printf("Register: Неверный формат телефона для %s: %s (нормализованный: %s)", input.Email, input.Phone, normalizedPhone)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат телефона (ожидается +7XXXXXXXXXX, 7XXXXXXXXXX или +7 (XXX) XXX-XX-XX)"})
			return
		}

		// Простая валидация Telegram (например, @username или username)
		telegramRegex := regexp.MustCompile(`^@?[a-zA-Z0-9_]{5,32}$`)
		if !telegramRegex.MatchString(input.Telegram) {
			log.Printf("Register: Неверный формат Telegram для %s: %s", input.Email, input.Telegram)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат Telegram (ожидается @username или username, 5-32 символов)"})
			return
		}

		log.Printf("Register: Получены данные для регистрации: %s", input.Email)

		// Нормализуем email
		normalizedEmail := strings.ToLower(input.Email)

		// Проверяем, существует ли пользователь с таким email
		var existingUser models.User
		if err := db.Where("email = ?", normalizedEmail).First(&existingUser).Error; err == nil {
			log.Printf("Register: Пользователь с email %s уже существует", normalizedEmail)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Пользователь с таким email уже существует"})
			return
		}

		// Создаём нового пользователя
		user := models.User{
			Name:     input.Name,
			Email:    normalizedEmail,
			Phone:    input.Phone, // Сохраняем форматированный номер
			Telegram: input.Telegram,
		}

		// Устанавливаем и хешируем пароль
		if err := user.SetPassword(input.Password); err != nil {
			log.Printf("Register: Ошибка установки пароля для %s: %v", input.Email, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Сохраняем пользователя в базе данных
		if err := db.Create(&user).Error; err != nil {
			log.Printf("Register: Ошибка создания пользователя %s: %v", input.Email, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка создания пользователя"})
			return
		}

		// Сохраняем сессию
		session := sessions.Default(c)
		session.Set("user_id", user.ID)
		if err := session.Save(); err != nil {
			log.Printf("Register: Ошибка сохранения сессии для пользователя %s: %v", user.Email, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка сохранения сессии"})
			return
		}

		log.Printf("Register: Пользователь успешно зарегистрирован: %s (ID: %d), сессия: %+v", user.Email, user.ID, session)
		c.JSON(http.StatusCreated, gin.H{
			"message":  "Регистрация прошла успешно",
			"redirect": "/dashboard",
		})
	}
}
