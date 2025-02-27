package handlers

import (
	"log"
	"net/http"
	"time"

	"github.com/MoshKillaPit/GrimishJe/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AdminStatsPage рендерит HTML-файл "admin_stats.html".
func AdminStatsPage(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.HTML(http.StatusOK, "admin_stats.html", gin.H{})
	}
}

// AdminStatsAll отдаёт JSON с фильтрацией по датам
func AdminStatsAll(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		startDateStr := c.Query("start_date") // формат: YYYY-MM-DD
		endDateStr := c.Query("end_date")     // формат: YYYY-MM-DD

		var startDate, endDate time.Time
		var err error

		if startDateStr != "" {
			startDate, err = time.Parse("2006-01-02", startDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат start_date"})
				return
			}
		}
		if endDateStr != "" {
			endDate, err = time.Parse("2006-01-02", endDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат end_date"})
				return
			}
		}

		query := db.Table("orders")
		if !startDate.IsZero() {
			query = query.Where("created_at >= ?", startDate)
		}
		if !endDate.IsZero() {
			query = query.Where("created_at <= ?", endDate)
		}

		// 1) Подсчёт заказов по статусам
		var counts []struct {
			Status string
			Count  int
		}
		if err := query.Select("status, COUNT(*) as count").Group("status").Scan(&counts).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка подсчёта статусов"})
			return
		}
		statusStats := make(map[string]int)
		for _, row := range counts {
			statusStats[row.Status] = row.Count
		}

		// 2) Суммарный бюджет по дням
		type BudgetRow struct {
			Date        string
			TotalBudget float64
		}
		var budgetRows []BudgetRow
		if err := query.Select("DATE(created_at) as date, SUM(budget) as total_budget").
			Group("DATE(created_at)").Scan(&budgetRows).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка подсчёта бюджета"})
			return
		}

		// 3) Количество новых заказов по дням
		type NewOrdersRow struct {
			Date  string
			Count int
		}
		var newOrdersRows []NewOrdersRow
		if err := query.Select("DATE(created_at) as date, COUNT(*) as count").
			Group("DATE(created_at)").Scan(&newOrdersRows).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка подсчёта новых заказов"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"statusStats":    statusStats,
			"budgetStats":    budgetRows,
			"newOrdersStats": newOrdersRows,
		})
	}
}

// GetBudgetStats с фильтрацией по датам
func GetBudgetStats(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		startDateStr := c.Query("start_date")
		endDateStr := c.Query("end_date")

		var startDate, endDate time.Time
		var err error

		if startDateStr != "" {
			startDate, err = time.Parse("2006-01-02", startDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат start_date"})
				return
			}
		}
		if endDateStr != "" {
			endDate, err = time.Parse("2006-01-02", endDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат end_date"})
				return
			}
		}

		type StatItem struct {
			Date        string  `json:"date"`
			TotalBudget float64 `json:"totalBudget"`
		}
		var results []StatItem

		query := db.Model(&models.Order{}).
			Select("DATE(created_at) as dt, SUM(budget) as total").
			Group("DATE(created_at)")

		if !startDate.IsZero() {
			query = query.Where("created_at >= ?", startDate)
		}
		if !endDate.IsZero() {
			query = query.Where("created_at <= ?", endDate)
		}

		rows, err := query.Rows()
		if err != nil {
			log.Printf("Ошибка запроса к БД для статистики: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка запроса к статистике"})
			return
		}
		defer rows.Close()

		for rows.Next() {
			var dt time.Time
			var total float64
			if err := rows.Scan(&dt, &total); err != nil {
				log.Printf("Ошибка чтения строки статистики: %v", err)
				continue
			}
			results = append(results, StatItem{
				Date:        dt.Format("2006-01-02"),
				TotalBudget: total,
			})
		}

		c.JSON(http.StatusOK, results)
	}
}
