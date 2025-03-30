// internal/handlers/status.go
package handlers

import (
	"net/http"
	"strconv"

	"github.com/MoshKillaPit/GrimishJe/internal/models"
	"github.com/MoshKillaPit/GrimishJe/internal/websocket"
	"github.com/gin-gonic/gin"
)

// GetOrderStatus возвращает текущий статус заказа.
// Если в хабе для данного заказа нет подключённых администраторов,
// возвращается статус "Оффлайн" с указанием времени последнего входа администратора.
func GetOrderStatus(hub *websocket.Hub) gin.HandlerFunc {
	return func(c *gin.Context) {
		orderIDStr := c.Query("orderID")
		if orderIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Отсутствует orderID"})
			return
		}
		orderID, err := strconv.ParseUint(orderIDStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат orderID"})
			return
		}

		status := "Оффлайн"
		hub.Mutex.RLock()
		if clients, ok := hub.Clients[uint(orderID)]; ok {
			for client := range clients {
				if client.IsAdmin {
					status = "Онлайн"
					break
				}
			}
		}
		hub.Mutex.RUnlock()

		// Если статус офлайн, добавляем время последнего входа администратора
		if status == "Оффлайн" {
			var admin models.User
			// Выбираем админа, у которого поле LastLogin не равно нулевому значению
			if err := hub.DB.Where("is_admin = ? AND last_login != ?", true, "0001-01-01 00:00:00").
				Order("last_login desc").
				First(&admin).Error; err == nil {
				if admin.LastLogin.IsZero() {
					status = "Оффлайн (был в сети: неизвестно)"
				} else {
					lastLogin := admin.LastLogin.Format("02.01.2006 15:04")
					status = "Оффлайн (был в сети: " + lastLogin + ")"
				}
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": status})
	}
}

// GetUserStatus возвращает текущий статус пользователя.
// Если нет активного соединения через WebSocket, берется время последнего входа из БД.
func GetUserStatus(hub *websocket.Hub) gin.HandlerFunc {
	return func(c *gin.Context) {
		userIDStr := c.Query("userID")
		if userIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Отсутствует userID"})
			return
		}
		userID, err := strconv.ParseUint(userIDStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат userID"})
			return
		}

		online := false
		// Перебираем все клиенты во всех заказах в хабе
		hub.Mutex.RLock()
		for _, clients := range hub.Clients {
			for client := range clients {
				// Если клиент соответствует указанному пользователю и не является админом – пользователь онлайн
				if client.UserID == uint(userID) && !client.IsAdmin {
					online = true
					break
				}
			}
			if online {
				break
			}
		}
		hub.Mutex.RUnlock()

		var status string
		if online {
			status = "Онлайн"
		} else {
			// Если нет активного WebSocket-соединения, берем время последнего входа из БД
			var user models.User
			if err := hub.DB.First(&user, uint(userID)).Error; err == nil {
				formatted := user.LastLogin.Format("02.01.2006 15:04")
				if user.LastLogin.IsZero() || formatted == "01.01.0001 00:00" {
					status = "Оффлайн (был в сети: неизвестно)"
				} else {
					status = "Оффлайн (был в сети: " + formatted + ")"
				}
			} else {
				status = "Оффлайн"
			}

		}
		c.JSON(http.StatusOK, gin.H{"status": status})
	}
}
