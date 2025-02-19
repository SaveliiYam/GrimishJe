package main

import (
	"log"
	"net/http"
	"strconv"

	"github.com/MoshKillaPit/GrimishJe/internal/handlers"
	"github.com/MoshKillaPit/GrimishJe/internal/middleware"
	"github.com/MoshKillaPit/GrimishJe/internal/models"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func initDB() *gorm.DB {
	// Измените параметры подключения при необходимости.
	dsn := "host=localhost user=postgres password=secret123 dbname=mydb port=5432 sslmode=disable TimeZone=Europe/Moscow"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("Не удалось подключиться к базе данных:", err)
	}

	// Миграция моделей: User, Order и ChatMessage
	if err := db.AutoMigrate(&models.User{}, &models.Order{}, &models.ChatMessage{}); err != nil {
		log.Fatal("Ошибка миграции:", err)
	}
	return db
}

func main() {
	db := initDB()
	// Инициализация MinIO для загрузки файлов
	handlers.InitMinio()

	r := gin.Default()

	// Инициализация сессий
	store := cookie.NewStore([]byte("super-secret-key"))
	r.Use(sessions.Sessions("mysession", store))

	// Отдаем статические файлы и шаблоны
	r.Static("/static", "../static")
	r.Static("/images", "../static/images")
	r.LoadHTMLGlob("../static/*.html")

	// Главная и информационные страницы
	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{"title": "Главная страница"})
	})
	r.GET("/about", func(c *gin.Context) {
		c.HTML(http.StatusOK, "about.html", gin.H{"title": "О нас"})
	})
	r.GET("/reviews", func(c *gin.Context) {
		c.HTML(http.StatusOK, "reviews.html", gin.H{"title": "Отзывы"})
	})
	r.GET("/services", func(c *gin.Context) {
		c.HTML(http.StatusOK, "services.html", gin.H{"title": "Услуги"})
	})

	// Авторизация и регистрация
	r.POST("/login", middleware.Login(db))
	r.POST("/register", middleware.Register(db))

	// Эндпоинты для заказов (пользователь)
	r.POST("/api/order", middleware.AuthRequired(), handlers.CreateOrder(db))
	r.POST("/api/order/edit", middleware.AuthRequired(), handlers.EditOrder(db))

	// Эндпоинты для администраторских действий
	r.POST("/api/order/accept", middleware.AdminRequired(db), handlers.AcceptOrder(db))
	r.POST("/api/order/complete", middleware.AdminRequired(db), handlers.CompleteOrder(db))
	r.POST("/api/order/cancel", middleware.AdminRequired(db), handlers.CancelOrder(db))
	r.POST("/api/order/delete", middleware.AdminRequired(db), handlers.DeleteOrder(db))
	// Новый endpoint для редактирования заказа администратором
	r.POST("/api/order/admin-edit", middleware.AdminRequired(db), handlers.AdminEditOrder(db))

	// Эндпоинт для загрузки файлов через MinIO
	r.POST("/api/order/upload", middleware.AuthRequired(), handlers.UploadFiles())
	r.GET("/api/order/files", middleware.AuthRequired(), handlers.ListFiles())

	// Новый endpoint для получения истории чата (админ)
	r.GET("/api/order/chat_history", middleware.AdminRequired(db), handlers.ChatHistory(db))

	// Маршрут для админ-панели (все заказы)
	r.GET("/admin_dashboard", middleware.AdminRequired(db), handlers.AdminDashboard(db))

	// Dashboard для обычного пользователя (только его заказы)
	r.GET("/dashboard", middleware.AuthRequired(), func(c *gin.Context) {
		session := sessions.Default(c)
		userIDVal := session.Get("user_id")
		if userIDVal == nil {
			c.Redirect(http.StatusFound, "/")
			return
		}
		var userID uint
		switch v := userIDVal.(type) {
		case uint:
			userID = v
		case int:
			userID = uint(v)
		default:
			c.Redirect(http.StatusFound, "/")
			return
		}
		var orders []models.Order
		if err := db.Where("user_id = ?", userID).Find(&orders).Error; err != nil {
			c.String(http.StatusInternalServerError, "Ошибка получения заказов")
			return
		}
		var user models.User
		if err := db.First(&user, userID).Error; err != nil {
			c.String(http.StatusInternalServerError, "Ошибка получения данных пользователя")
			return
		}
		c.HTML(http.StatusOK, "dashboard.html", gin.H{
			"User":   user,
			"Orders": orders,
		})
	})

	// Страница заказа для пользователя (проверка принадлежности)
	r.GET("/order", middleware.AuthRequired(), func(c *gin.Context) {
		orderIDStr := c.Query("id")
		if orderIDStr == "" {
			c.String(http.StatusBadRequest, "Отсутствует ID заказа")
			return
		}
		orderID, err := strconv.ParseUint(orderIDStr, 10, 64)
		if err != nil {
			c.String(http.StatusBadRequest, "Неверный ID заказа")
			return
		}
		var order models.Order
		if err := db.First(&order, orderID).Error; err != nil {
			c.String(http.StatusNotFound, "Заказ не найден")
			return
		}
		session := sessions.Default(c)
		userIDVal := session.Get("user_id")
		var userID uint
		switch v := userIDVal.(type) {
		case uint:
			userID = v
		case int:
			userID = uint(v)
		default:
			c.String(http.StatusUnauthorized, "Пользователь не авторизован")
			return
		}
		if order.UserID != userID {
			c.String(http.StatusForbidden, "Нет доступа к этому заказу")
			return
		}
		c.HTML(http.StatusOK, "order.html", gin.H{"Order": order})
	})

	// Маршрут для WebSocket-чата для администратора
	r.GET("/ws/chat", middleware.AdminRequired(db), handlers.ChatHandler(db))
	// Маршрут для WebSocket-чата для пользователей
	r.GET("/ws/chat_user", middleware.AuthRequired(), handlers.ChatHandler(db))

	// Запускаем чат-хаб
	handlers.RunChatHub()

	if err := r.Run(":8080"); err != nil {
		log.Fatal("Не удалось запустить сервер:", err)
	}
}
