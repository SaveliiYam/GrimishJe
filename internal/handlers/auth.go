package handlers

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

// Register обрабатывает регистрацию пользователя, сохраняет сессию и отправляет JSON-ответ.
func Register(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			Name     string `form:"name" binding:"required"`
			Email    string `form:"email" binding:"required,email"`
			Password string `form:"password" binding:"required,min=6"`
			Phone    string `form:"phone" binding:"required"`
			Telegram string `form:"telegram" binding:"required"`
		}

		// Привязываем данные формы
		if err := c.ShouldBind(&input); err != nil {
			log.Printf("Register: Ошибка привязки данных: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверные данные формы: " + err.Error()})
			return
		}

		// Простая валидация формата телефона (например, +79991234567 или 89991234567)
		phoneRegex := regexp.MustCompile(`^(?:\+7|8)\d{10}$`)
		if !phoneRegex.MatchString(input.Phone) {
			log.Printf("Register: Неверный формат телефона для %s: %s", input.Email, input.Phone)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат телефона (ожидается +79991234567 или 89991234567)"})
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

		// Создаём нового пользователя
		user := models.User{
			Name:     input.Name,
			Email:    strings.ToLower(input.Email), // Нормализуем email
			Phone:    input.Phone,
			Telegram: input.Telegram,
		}

		// Устанавливаем и хешируем пароль
		if err := user.SetPassword(input.Password); err != nil {
			log.Printf("Register: Ошибка установки пароля для %s: %v", input.Email, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка обработки пароля"})
			return
		}

		// Сохраняем пользователя в базе данных
		if err := db.Create(&user).Error; err != nil {
			log.Printf("Register: Ошибка создания пользователя %s: %v", input.Email, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Пользователь с таким email уже существует"})
			return
		}

		// Сохраняем сессию
		session := sessions.Default(c)
		session.Set("user_id", user.ID)
		if err := session.Save(); err != nil {
			log.Printf("Register: Ошибка сохранения сессии для %s: %v", user.Email, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка сохранения сессии"})
			return
		}

		log.Printf("Register: Пользователь успешно зарегистрирован: %s", user.Email)
		c.JSON(http.StatusCreated, gin.H{
			"message":  "Регистрация прошла успешно",
			"redirect": "/dashboard", // Фронтенд может сам обрабатывать редирект
		})
	}
}

// Login обрабатывает вход пользователя, сохраняет сессию и отправляет JSON-ответ.
func Login(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			Email    string `form:"email" binding:"required,email"`
			Password string `form:"password" binding:"required"`
		}

		// Привязываем данные формы
		if err := c.ShouldBind(&input); err != nil {
			log.Printf("Login: Ошибка привязки данных: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверные данные формы: " + err.Error()})
			return
		}

		// Ищем пользователя по email (нормализуем email для поиска)
		var user models.User
		if err := db.Where("email = ?", strings.ToLower(input.Email)).First(&user).Error; err != nil {
			log.Printf("Login: Пользователь с email %s не найден: %v", input.Email, err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Неверный email или пароль"})
			return
		}

		// Проверяем пароль
		if !user.CheckPassword(input.Password) {
			log.Printf("Login: Неверный пароль для пользователя %s", user.Email)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Неверный email или пароль"})
			return
		}

		// Сохраняем сессию
		session := sessions.Default(c)
		session.Set("user_id", user.ID)
		if err := session.Save(); err != nil {
			log.Printf("Login: Ошибка сохранения сессии для %s: %v", user.Email, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка сохранения сессии"})
			return
		}

		log.Printf("Login: Пользователь успешно вошел: %s", user.Email)
		c.JSON(http.StatusOK, gin.H{
			"message":  "Вход выполнен успешно",
			"redirect": "/dashboard", // Для обычного пользователя
		})
		if user.IsAdmin {
			c.JSON(http.StatusOK, gin.H{
				"message":  "Вход выполнен успешно",
				"redirect": "/admin_dashboard", // Для администратора
			})
		}
	}
}

func AuthStatus(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")
		if userID == nil {
			c.JSON(http.StatusOK, gin.H{"user": nil})
			return
		}
		var userIDUint uint
		switch v := userID.(type) {
		case uint:
			userIDUint = v
		case int:
			userIDUint = uint(v)
		case string:
			parsed, err := strconv.ParseUint(v, 10, 32)
			if err != nil {
				log.Printf("AuthStatus: Ошибка преобразования user_id: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Неверный формат user_id"})
				return
			}
			userIDUint = uint(parsed)
		default:
			log.Println("AuthStatus: Неподдерживаемый тип user_id в сессии")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Неверный тип user_id"})
			return
		}
		var user models.User
		if err := db.First(&user, userIDUint).Error; err != nil {
			log.Printf("AuthStatus: Ошибка получения данных пользователя с ID %d: %v", userIDUint, err)
			c.JSON(http.StatusOK, gin.H{"user": nil})
			return
		}
		c.JSON(http.StatusOK, gin.H{"user": user})
	}
}
