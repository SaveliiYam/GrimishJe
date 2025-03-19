package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"

	"github.com/MoshKillaPit/GrimishJe/internal/handlers"
	"github.com/MoshKillaPit/GrimishJe/internal/middleware"
	"github.com/MoshKillaPit/GrimishJe/internal/models"
	"github.com/MoshKillaPit/GrimishJe/internal/websocket"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func loadConfig() (*handlers.Config, string) {
	if err := godotenv.Load("../.env"); err != nil {
		log.Println("Не удалось загрузить ../.env файл, используются переменные окружения")
	}

	return &handlers.Config{
		MinioEndpoint:  os.Getenv("MINIO_ENDPOINT"),
		MinioAccessKey: os.Getenv("MINIO_ACCESS_KEY"),
		MinioSecretKey: os.Getenv("MINIO_SECRET_KEY"),
		MinioBucket:    os.Getenv("MINIO_BUCKET"),
	}, os.Getenv("SESSION_SECRET")
}

func initDB() *gorm.DB {
	host := os.Getenv("DB_HOST")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")
	port := os.Getenv("DB_PORT")
	sslMode := os.Getenv("DB_SSLMODE")
	timezone := os.Getenv("DB_TIMEZONE")

	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=%s",
		host, user, password, dbName, port, sslMode, timezone,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Не удалось подключиться к базе данных: %v", err)
	}

	if err := db.AutoMigrate(&models.User{}, &models.Order{}, &models.Review{}, &models.File{}, &models.ChatMessage{}); err != nil {
		log.Fatalf("Ошибка миграции БД: %v", err)
	}

	log.Println("Успешно подключено к базе данных и выполнена миграция")
	return db
}

func main() {
	cfg, sessionSecret := loadConfig()
	db := initDB()

	// Инициализация MinIO
	minioClient, bucketName, err := handlers.InitMinio(db, cfg)
	if err != nil {
		log.Fatalf("Ошибка инициализации MinIO: %v", err)
	}

	// Инициализация Gin
	r := gin.Default()

	// Мидлвара для передачи db в контекст
	r.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Next()
	})

	// Настройка шаблонизатора
	r.SetFuncMap(template.FuncMap{
		"formatDate": func(t time.Time) string {
			return t.Format("2006-01-02")
		},
		"formatDateString": func(dateStr string) string {
			if dateStr == "" {
				return "Не указано"
			}
			layouts := []string{"2006-01-02T15:04:05Z", "2006-01-02"}
			var t time.Time
			var err error
			for _, layout := range layouts {
				t, err = time.Parse(layout, dateStr)
				if err == nil {
					break
				}
			}
			if err != nil {
				log.Printf("Ошибка парсинга даты %s: %v", dateStr, err)
				return dateStr
			}
			return t.Format("02.01.2006")
		},
		"sub": func(a, b float64) float64 {
			return a - b
		},
	})

	// Настройка сессий
	if sessionSecret == "" {
		log.Fatal("SESSION_SECRET не задана в окружении")
	}
	store := cookie.NewStore([]byte(sessionSecret))
	r.Use(sessions.Sessions("mysession", store))

	// Статические файлы и шаблоны
	r.Static("/static", "../static")
	r.Static("/images", "../static/images")
	r.LoadHTMLGlob("../static/*.html")

	// Инициализация и запуск WebSocket-хаба
	wsHub := websocket.NewHub(db)
	go wsHub.Run()
	handlers.SetWebSocketHub(wsHub)

	// Маршруты для WebSocket
	r.GET("/ws/chat", middleware.AuthRequired(), websocket.WebSocketHandler(wsHub))
	r.GET("/ws/chat_user", middleware.AuthRequired(), websocket.WebSocketHandler(wsHub))

	// Маршруты
	// Изменённый маршрут для детальной страницы заказа для администратора
	r.GET("/admin/order/:id", middleware.AdminRequired(db), handlers.GetAdminOrder(db))
	r.GET("/", func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")
		var user *models.User

		if userID != nil {
			var userIDUint uint
			switch v := userID.(type) {
			case uint:
				userIDUint = v
			case int:
				userIDUint = uint(v)
			case string:
				parsed, err := strconv.ParseUint(v, 10, 32)
				if err == nil {
					userIDUint = uint(parsed)
				}
			}
			if userIDUint > 0 {
				if err := db.First(&user, userIDUint).Error; err != nil {
					log.Printf("Ошибка получения пользователя: %v", err)
					user = nil
				}
			}
		}

		c.HTML(http.StatusOK, "index.html", gin.H{
			"User": user,
			"Year": time.Now().Year(),
		})
	})
	r.GET("/about", func(c *gin.Context) {
		c.HTML(http.StatusOK, "about.html", gin.H{"title": "О нас"})
	})

	r.GET("/api/user/status", handlers.GetUserStatus(wsHub))

	r.GET("/api/admin/last_login", middleware.AdminRequired(db), handlers.GetAdminLastLogin(db))

	r.GET("/api/order/status", handlers.GetOrderStatus(wsHub))

	r.POST("/api/order/delete-file", middleware.AuthRequired(), func(c *gin.Context) {
		handlers.DeleteFile(c, db, minioClient, bucketName)
	})

	r.GET("/reviews", func(c *gin.Context) {
		c.HTML(http.StatusOK, "reviews.html", gin.H{"title": "Отзывы"})
	})
	r.GET("/services", func(c *gin.Context) {
		c.HTML(http.StatusOK, "services.html", gin.H{"title": "Услуги"})
	})
	r.POST("/login", middleware.Login(db))
	r.POST("/register", middleware.Register(db))
	r.POST("/api/order", middleware.AuthRequired(), handlers.CreateOrder(db))
	r.POST("/api/order/edit", middleware.AuthRequired(), handlers.EditOrder(db))
	r.POST("/api/order/accept", middleware.AdminRequired(db), handlers.AcceptOrder(db))
	r.POST("/api/order/complete", middleware.AdminRequired(db), handlers.CompleteOrder(db))
	r.POST("/api/order/cancel", middleware.AdminRequired(db), handlers.CancelOrder(db))
	r.POST("/api/order/delete", middleware.AdminRequired(db), handlers.DeleteOrder(db))
	r.POST("/api/order/admin-edit", middleware.AdminRequired(db), handlers.AdminEditOrder(db))
	r.POST("/api/order/review", middleware.AuthRequired(), handlers.CreateReview(db))
	r.POST("/api/order/upload", middleware.AuthRequired(), handlers.UploadFiles(db, minioClient, bucketName))
	r.GET("/api/order/files", middleware.AuthRequired(), func(c *gin.Context) {
		handlers.ListFiles(c, db, minioClient, bucketName)
	})
	r.GET("/api/order/chat_history", middleware.AuthRequired(), handlers.ChatHistory(db))
	r.GET("/admin_dashboard", middleware.AdminRequired(db), handlers.AdminDashboard(db))
	r.GET("/api/admin/orders", middleware.AdminRequired(db), handlers.ListAdminOrders(db))
	r.GET("/api/order/review", middleware.AuthRequired(), handlers.GetReview(db))
	r.GET("/dashboard", middleware.AuthRequired(), func(c *gin.Context) {
		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil {
			c.Redirect(http.StatusFound, "/")
			return
		}

		var userID uint
		switch v := uid.(type) {
		case uint:
			userID = v
		case int:
			userID = uint(v)
		default:
			c.Redirect(http.StatusFound, "/")
			return
		}

		var user models.User
		if err := db.First(&user, userID).Error; err != nil {
			c.String(http.StatusInternalServerError, "Ошибка получения данных пользователя")
			return
		}

		var orders []models.Order
		if err := db.Where("user_id = ?", userID).Find(&orders).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.HTML(http.StatusOK, "dashboard.html", gin.H{
					"User":   user,
					"Orders": nil,
				})
				return
			}
			c.String(http.StatusInternalServerError, "Ошибка получения заказов")
			return
		}

		c.HTML(http.StatusOK, "dashboard.html", gin.H{
			"User":   user,
			"Orders": orders,
		})
	})
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
		if err := db.First(&order, uint(orderID)).Error; err != nil {
			c.String(http.StatusNotFound, "Заказ не найден")
			return
		}

		session := sessions.Default(c)
		userIDVal := session.Get("user_id")
		if userIDVal == nil {
			c.String(http.StatusUnauthorized, "Пользователь не авторизован")
			return
		}

		var userID uint
		switch v := userIDVal.(type) {
		case uint:
			userID = v
		case int:
			userID = uint(v)
		default:
			c.String(http.StatusUnauthorized, "Неверный формат user_id")
			return
		}

		if order.UserID != userID {
			c.String(http.StatusForbidden, "Нет доступа к этому заказу")
			return
		}

		c.HTML(http.StatusOK, "order.html", gin.H{"Order": order})
	})
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "http://localhost:8080")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})
	r.GET("/admin_stats", middleware.AdminRequired(db), handlers.AdminStatsPage(db))
	r.GET("/api/admin/stats/all", middleware.AdminRequired(db), handlers.AdminStatsAll(db))
	r.GET("/api/admin/stats/budget", middleware.AdminRequired(db), handlers.GetBudgetStats(db))
	r.POST("/api/admin/upload-final-file", middleware.AdminRequired(db), func(c *gin.Context) {
		handlers.UploadFinalFile(c, db, minioClient, bucketName)
	})

	r.GET("/favicon.ico", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	r.GET("/cdn-cgi/challenge-platform/scripts/jsd/main.js", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	r.GET("/api/reviews", handlers.GetReviews(db))
	r.POST("/api/order/update-payment", middleware.AdminRequired(db), handlers.UpdatePaymentStatus(db))

	// Запуск сервера
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Запуск сервера на порту %s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Не удалось запустить сервер: %v", err)
	}
}
