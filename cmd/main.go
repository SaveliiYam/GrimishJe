package main

import (
	"log"
	"net/http"

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
	// Строка подключения – измените параметры по необходимости.
	dsn := "host=localhost user=postgres password=secret123 dbname=mydb port=5432 sslmode=disable TimeZone=Europe/Moscow"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("Не удалось подключиться к базе данных:", err)
	}

	// Миграция моделей пользователя и заказа
	if err := db.AutoMigrate(&models.User{}, &models.Order{}); err != nil {
		log.Fatal("Ошибка миграции:", err)
	}
	return db
}

func main() {
	db := initDB()
	r := gin.Default()

	// Инициализация хранилища сессий
	store := cookie.NewStore([]byte("super-secret-key"))
	r.Use(sessions.Sessions("mysession", store))

	// Раздача статических файлов только по /static
	r.Static("/static", "./static")

	// Определяем маршрут для главной страницы (корневой адрес)
	r.LoadHTMLFiles("./static/dashboard.html")
	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "dashboard.html", gin.H{
			"title": "Личный кабинет",
		})
	})

	// Маршруты для авторизации и регистрации
	r.POST("/login", middleware.Login(db))
	r.POST("/register", middleware.Register(db))

	// Защищённые маршруты
	r.POST("/api/order", middleware.AuthRequired(), handlers.CreateOrder(db))
	r.GET("/dashboard", middleware.AuthRequired(), handlers.Dashboard())

	// Запуск сервера
	if err := r.Run(":8080"); err != nil {
		log.Fatal("Не удалось запустить сервер:", err)
	}
}
