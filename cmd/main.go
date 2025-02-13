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
	// Измените параметры подключения при необходимости.
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

	// Отдаем статические файлы:
	// Все файлы из папки ../static доступны по URL /static (в т.ч. CSS, JS и пр.)
	r.Static("/static", "../static")
	// Отдаем изображения отдельно: файлы из ../static/images доступны по URL /images
	r.Static("/images", "../static/images")

	// Загружаем HTML-шаблоны из папки static
	// Если вы запускаете из папки cmd, шаблоны расположены на уровень выше: ../static/*.html
	r.LoadHTMLGlob("../static/*.html")

	// Маршрут для главной страницы (index.html)
	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{
			"title": "Главная страница",
		})
	})

	// Дополнительные маршруты для остальных страниц
	r.GET("/about", func(c *gin.Context) {
		c.HTML(http.StatusOK, "about.html", gin.H{
			"title": "О нас",
		})
	})

	r.GET("/reviews", func(c *gin.Context) {
		c.HTML(http.StatusOK, "reviews.html", gin.H{
			"title": "Отзывы",
		})
	})

	r.GET("/services", func(c *gin.Context) {
		c.HTML(http.StatusOK, "services.html", gin.H{
			"title": "Услуги",
		})
	})

	// Маршруты для авторизации и регистрации
	r.POST("/login", middleware.Login(db))
	r.POST("/register", middleware.Register(db))

	// Маршруты, требующие авторизации
	r.POST("/api/order", middleware.AuthRequired(), handlers.CreateOrder(db))
	r.GET("/dashboard", middleware.AuthRequired(), handlers.Dashboard())

	// Запуск сервера на порту 8080
	if err := r.Run(":8080"); err != nil {
		log.Fatal("Не удалось запустить сервер:", err)
	}
}
