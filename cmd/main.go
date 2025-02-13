package main

import (
	"log"

	"MySite/handlers"
	"MySite/middleware"
	"MySite/models"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin" // можно использовать другой драйвер (postgres, mysql и т.д.)
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func initDB() *gorm.DB {
	// Формируем строку подключения (DSN). Измените параметры в соответствии с настройками вашей базы данных.
	dsn := "host=localhost user=postgres password=secret123 dbname=mydb port=5432 sslmode=disable TimeZone=Europe/Moscow"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("Не удалось подключиться к базе данных:", err)
	}

	// Автоматическая миграция модели пользователя (и других, если необходимо)
	if err := db.AutoMigrate(&models.User{}); err != nil {
		log.Fatal("Ошибка миграции:", err)
	}
	return db
}

func main() {
	db := initDB()

	r := gin.Default()

	// Инициализация хранилища сессий с секретным ключом
	store := cookie.NewStore([]byte("super-secret-key"))
	r.Use(sessions.Sessions("mysession", store))

	// Раздача статических файлов (например, для главного сайта и dashboard)
	r.Static("/", "./static")

	// Группа API для авторизации
	api := r.Group("/api")
	{
		api.POST("/register", handlers.Register(db))
		api.POST("/login", handlers.Login(db))
	}

	// Защищённый маршрут (только для авторизованных пользователей)
	r.GET("/dashboard", middleware.AuthRequired(), handlers.Dashboard())

	// Если хотите сделать редирект сразу после успешной авторизации или регистрации,
	// можно использовать метод c.Redirect внутри обработчиков.

	// Запуск сервера
	if err := r.Run(":8080"); err != nil {
		log.Fatal("Не удалось запустить сервер:", err)
	}
}
