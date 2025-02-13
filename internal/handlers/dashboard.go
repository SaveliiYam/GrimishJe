package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Dashboard – рендерит HTML-страницу личного кабинета (dashboard.html)
func Dashboard() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.HTML(http.StatusOK, "dashboard.html", gin.H{
			"title": "Личный кабинет",
		})
	}
}
