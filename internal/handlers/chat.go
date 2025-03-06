package handlers

import (
	"github.com/MoshKillaPit/GrimishJe/internal/websocket"
	"github.com/gin-gonic/gin"
)

// ChatHandler перенаправляет запросы на WebSocket-обработчик
func ChatHandler(wsHub *websocket.Hub) gin.HandlerFunc {
	return func(c *gin.Context) {
		websocket.WebSocketHandler(wsHub)(c)
	}
}

// RunChatHub инициализирует и запускает WebSocket-хаб
func RunChatHub(wsHub *websocket.Hub) {
	// Хаб уже запускается в main.go
}
