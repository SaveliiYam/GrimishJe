package handlers

import (
	"log"
	"net/http"
	"strconv"

	"github.com/MoshKillaPit/GrimishJe/internal/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AdminDashboard рендерит админ-панель
func AdminDashboard(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil {
			log.Println("Неавторизованный доступ к админ-панели")
			c.Redirect(http.StatusFound, "/")
			return
		}

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
				log.Printf("Ошибка преобразования user_id: %v", err)
				c.String(http.StatusInternalServerError, "Неверный формат user_id")
				return
			}
			userID = uint(parsed)
		default:
			log.Println("Неподдерживаемый тип user_id")
			c.String(http.StatusInternalServerError, "Неверный формат user_id")
			return
		}

		// Дополнительная проверка роли администратора
		var user models.User
		if err := db.First(&user, userID).Error; err != nil {
			log.Printf("Ошибка получения пользователя ID=%d: %v", userID, err)
			c.String(http.StatusInternalServerError, "Ошибка получения данных пользователя")
			return
		}
		if !user.IsAdmin {
			log.Println("Попытка доступа к админ-панели неадминистратором")
			c.String(http.StatusForbidden, "Доступ запрещён: требуется роль администратора")
			return
		}

		// Получаем все заказы с дополнительным логированием
		var orders []models.Order
		if err := db.Preload("User").Find(&orders).Error; err != nil {
			log.Printf("Ошибка получения заказов для админ-панели: %v", err)
			c.String(http.StatusInternalServerError, "Ошибка получения заказов")
			return
		}
		log.Printf("Получено %d заказов для админ-панели, первый заказ: %+v", len(orders), orders[0])

		// Передаём заказы в шаблон
		c.HTML(http.StatusOK, "admin_dashboard.html", gin.H{
			"User":   user,
			"Orders": orders,
		})
	}
}

// ListAdminOrders возвращает список заказов для админ-панели через API
func ListAdminOrders(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Не авторизован"})
			return
		}

		var user models.User
		if err := db.First(&user, uid).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Пользователь не найден"})
			return
		}

		if !user.IsAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "Нет доступа"})
			return
		}

		var orders []models.Order
		if err := db.Preload("User").Find(&orders).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка получения заказов"})
			return
		}

		responseOrders := make([]gin.H, len(orders))
		for i, order := range orders {
			userInfo := gin.H{}
			if order.User.ID != 0 { // Проверяем, загружен ли User
				userInfo = gin.H{
					"Name":     order.User.Name,
					"Phone":    order.User.Phone,
					"Telegram": order.User.Telegram,
				}
			}

			responseOrders[i] = gin.H{
				"ID":                 order.ID,
				"OrderNumber":        order.OrderNumber,
				"User":               userInfo,
				"Topic":              order.Topic,
				"Deadline":           order.Deadline,
				"PlagiarismRequired": order.PlagiarismRequired,
				"PlagiarismPercent":  order.PlagiarismPercent,
				"Budget":             order.Budget,
				"Status":             order.Status,
				"WorkType":           order.WorkType,
				"Notes":              order.Notes,
				"FinalFileURL":       order.FinalFileURL, // Добавляем поле в ответ
				"CreatedAt":          order.CreatedAt.Format("2006-01-02"),
			}
		}

		log.Printf("Возвращено %d заказов через API /api/admin/orders", len(orders))
		c.JSON(http.StatusOK, gin.H{"orders": responseOrders})
	}
}

// GetOrderReview возвращает отзыв для определённого заказа.
func GetOrderReview(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		orderIDStr := c.Query("orderID")
		if orderIDStr == "" {
			log.Println("Отсутствует orderID в запросе отзыва")
			c.JSON(http.StatusBadRequest, gin.H{"error": "Отсутствует orderID"})
			return
		}

		orderID, err := strconv.ParseUint(orderIDStr, 10, 64)
		if err != nil {
			log.Printf("GetOrderReview: Неверный format orderID: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
			return
		}

		var review models.Review
		if err := db.Where("order_id = ?", uint(orderID)).Preload("User").First(&review).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				log.Printf("GetOrderReview: Отзыв для заказа %d не найден", orderID)
				c.JSON(http.StatusOK, gin.H{"review": nil})
				return
			}
			log.Printf("GetOrderReview: Ошибка получения отзыва для заказа %d: %v", orderID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка получения отзыва"})
			return
		}

		responseReview := gin.H{
			"ID":      review.ID,
			"OrderID": review.OrderID,
			"User":    gin.H{"Name": review.User.Name},
			"Rating":  review.Rating,
			"Comment": review.Comment,
		}

		log.Printf("Отзыв для заказа %d успешно возвращён", orderID)
		c.JSON(http.StatusOK, gin.H{"review": responseReview})
	}
}

func GetAdminLastLogin(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Получаем администратора (предполагается, что админ один)
		var admin models.User
		if err := db.Where("is_admin = ?", true).First(&admin).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Администратор не найден"})
			return
		}

		// Форматируем время последнего входа администратора
		lastLogin := admin.LastLogin.Format("02.01.2006 15:04")
		c.JSON(http.StatusOK, gin.H{"lastLogin": lastLogin})
	}
}
