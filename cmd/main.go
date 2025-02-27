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

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Config хранит конфигурацию приложения.
type Config struct {
	DBHost         string
	DBUser         string
	DBPassword     string
	DBName         string
	DBPort         string
	DBSSLMode      string
	DBTimezone     string
	SessionSecret  string
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	MinioBucket    string
}

// loadConfig загружает переменные окружения из .env (при наличии).
func loadConfig() *Config {
	// Пытаемся загрузить .env (либо ../.env)
	if err := godotenv.Load("../.env"); err != nil {
		log.Println("Не удалось загрузить ../.env файл, используются переменные окружения")
	}

	return &Config{
		DBHost:         os.Getenv("DB_HOST"),
		DBUser:         os.Getenv("DB_USER"),
		DBPassword:     os.Getenv("DB_PASSWORD"),
		DBName:         os.Getenv("DB_NAME"),
		DBPort:         os.Getenv("DB_PORT"),
		DBSSLMode:      os.Getenv("DB_SSLMODE"),
		DBTimezone:     os.Getenv("DB_TIMEZONE"),
		SessionSecret:  os.Getenv("SESSION_SECRET"),
		MinioEndpoint:  os.Getenv("MINIO_ENDPOINT"),
		MinioAccessKey: os.Getenv("MINIO_ACCESS_KEY"),
		MinioSecretKey: os.Getenv("MINIO_SECRET_KEY"),
		MinioBucket:    os.Getenv("MINIO_BUCKET"),
	}
}

// initDB подключается к PostgreSQL и выполняет миграцию.
func initDB(cfg *Config) *gorm.DB {
	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=%s",
		cfg.DBHost, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBPort, cfg.DBSSLMode, cfg.DBTimezone,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Не удалось подключиться к базе данных: %v", err)
	}

	// Миграция всех моделей, включая Review
	if err := db.AutoMigrate(&models.User{}, &models.Order{}, &models.Review{}, &models.ChatMessage{}); err != nil {
		log.Fatalf("Ошибка миграции БД: %v", err)
	}

	log.Println("Успешно подключено к базе данных и выполнена миграция")
	return db
}

func main() {
	// 1. Загружаем конфиг
	cfg := loadConfig()

	// 2. Инициализируем БД
	db := initDB(cfg)

	// 3. Инициализируем MinIO (если используете)
	minioCfg := &handlers.Config{
		MinioEndpoint:  cfg.MinioEndpoint,
		MinioAccessKey: cfg.MinioAccessKey,
		MinioSecretKey: cfg.MinioSecretKey,
		MinioBucket:    cfg.MinioBucket,
	}
	handlers.InitMinio(db, minioCfg)

	// 4. Инициализируем Gin
	r := gin.Default()

	// 5. Мидлвара: кладём db в контекст, чтобы можно было доставать потом
	r.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Next()
	})

	// 6. Подключаем кастомные функции в шаблонизатор
	r.SetFuncMap(template.FuncMap{
		"formatDate": func(t time.Time) string {
			return t.Format("2006-01-02") // Формат для time.Time
		},
		"formatDateString": func(dateStr string) string {
			if dateStr == "" {
				return "Не указано"
			}
			// Пробуем парсить ISO 8601 (например, "2025-02-28T00:00:00Z") и простой формат YYYY-MM-DD
			layouts := []string{
				"2006-01-02T15:04:05Z", // ISO 8601 с Z
				"2006-01-02",           // Простой формат YYYY-MM-DD
			}
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
				return dateStr // Возвращаем исходную строку, если парсинг не удался
			}
			return t.Format("02.01.2006") // Вывод (DD.MM.YYYY)
		},
	})

	// 7. Сессии
	if cfg.SessionSecret == "" {
		log.Fatal("SESSION_SECRET не задана в окружении")
	}
	store := cookie.NewStore([]byte(cfg.SessionSecret))
	r.Use(sessions.Sessions("mysession", store))

	// 8. Статические файлы и шаблоны
	r.Static("/static", "../static")
	r.Static("/images", "../static/images")
	r.LoadHTMLGlob("../static/*.html")

	// Пример: дополнительный эндпоинт (AdminRequired) - получить заказ
	r.GET("/api/admin/order/:id", middleware.AdminRequired(db), handlers.GetAdminOrder(db))

	// 9. Главная страница
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

	// Примеры обычных страниц
	r.GET("/about", func(c *gin.Context) {
		c.HTML(http.StatusOK, "about.html", gin.H{"title": "О нас"})
	})
	r.GET("/reviews", func(c *gin.Context) {
		c.HTML(http.StatusOK, "reviews.html", gin.H{"title": "Отзывы"})
	})
	r.GET("/services", func(c *gin.Context) {
		c.HTML(http.StatusOK, "services.html", gin.H{"title": "Услуги"})
	})

	// 10. Авторизация и регистрация
	r.POST("/login", middleware.Login(db))
	r.POST("/register", middleware.Register(db))

	// 11. Эндпоинты для заказов (требуют авторизации)
	r.POST("/api/order", middleware.AuthRequired(), handlers.CreateOrder(db))
	r.POST("/api/order/edit", middleware.AuthRequired(), handlers.EditOrder(db))

	// 12. Эндпоинты для действий администратора
	r.POST("/api/order/accept", middleware.AdminRequired(db), handlers.AcceptOrder(db))
	r.POST("/api/order/complete", middleware.AdminRequired(db), handlers.CompleteOrder(db))
	r.POST("/api/order/cancel", middleware.AdminRequired(db), handlers.CancelOrder(db))
	r.POST("/api/order/delete", middleware.AdminRequired(db), handlers.DeleteOrder(db))
	r.POST("/api/order/admin-edit", middleware.AdminRequired(db), handlers.AdminEditOrder(db))
	r.POST("/api/order/review", middleware.AuthRequired(), handlers.CreateReview(db))

	// 13. Загрузка файлов
	r.POST("/api/order/upload", middleware.AuthRequired(), handlers.UploadFiles(db))
	r.GET("/api/order/files", middleware.AuthRequired(), handlers.ListFiles(db))

	// 14. История чата
	r.GET("/api/order/chat_history", middleware.AuthRequired(), handlers.ChatHistory(db))

	// 15. Админ-панель
	r.GET("/admin_dashboard", middleware.AdminRequired(db), handlers.AdminDashboard(db))
	// 16. Список заказов для админов (JSON)
	r.GET("/api/admin/orders", middleware.AdminRequired(db), handlers.ListAdminOrders(db))

	// 17. Новый маршрут для отзыва (требует авторизации, только для авторизованных пользователей)
	r.GET("/api/order/review", middleware.AuthRequired(), handlers.GetReview(db))

	// 18. Кабинет пользователя
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
				// Если заказов нет, показываем пустую страницу с предложением создать заказ
				c.HTML(http.StatusOK, "dashboard.html", gin.H{
					"User":   user,
					"Orders": nil, // Передаём nil, чтобы отобразить приветственное сообщение
				})
				return
			}
			c.String(http.StatusInternalServerError, "Ошибка получения заказов")
			return
		}

		// Если заказы есть, отображаем их
		c.HTML(http.StatusOK, "dashboard.html", gin.H{
			"User":   user,
			"Orders": orders,
		})
	})

	// 19. Страница одного заказа (пример)
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
		// Приводим orderID к типу uint, чтобы GORM корректно подобрал запись
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

	// 20. WebSocket-чаты
	r.GET("/ws/chat", middleware.AuthRequired(), handlers.ChatHandler(db))
	r.GET("/ws/chat_user", middleware.AuthRequired(), handlers.ChatHandler(db))
	handlers.RunChatHub()

	// 21. CORS (опционально)
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

	// Страница со статистикой
	r.GET("/admin_stats", middleware.AdminRequired(db), handlers.AdminStatsPage(db))
	r.GET("/api/admin/stats/all", middleware.AdminRequired(db), handlers.AdminStatsAll(db))
	r.GET("/api/admin/stats/budget", middleware.AdminRequired(db), handlers.GetBudgetStats(db))

	// 22. Запуск сервера
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Не удалось запустить сервер: %v", err)
	}
}
