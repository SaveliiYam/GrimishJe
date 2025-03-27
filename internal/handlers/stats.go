package handlers

import (
	"log"
	"net/http"
	"sort"
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

// AdminStatsAll отдаёт JSON со статистикой по заказам с фильтрацией по датам.
// При этом для подсчёта бюджета учитываются только заказы со статусом "завершён".
func AdminStatsAll(db *gorm.DB) gin.HandlerFunc {
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

		// Общий запрос без фильтра по статусу для подсчёта распределения заказов по статусам
		query := db.Table("orders")
		if !startDate.IsZero() {
			query = query.Where("created_at >= ?", startDate)
		}
		if !endDate.IsZero() {
			query = query.Where("created_at <= ?", endDate)
		}

		log.Printf("AdminStatsAll: Фильтры - startDate: %v, endDate: %v", startDate, endDate)

		// 1) Подсчёт заказов по статусам (без фильтра по статусу, чтобы отобразить распределение)
		var counts []struct {
			Status string
			Count  int
		}
		if err := query.Select("status, COUNT(*) as count").Group("status").Scan(&counts).Error; err != nil {
			log.Printf("AdminStatsAll: Ошибка подсчёта статусов: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка подсчёта статусов"})
			return
		}
		statusStats := make(map[string]int)
		for _, row := range counts {
			statusStats[row.Status] = row.Count
		}

		// 2) Суммарный бюджет по дням – учитываем только завершённые заказы.
		type BudgetRow struct {
			Date        string  `json:"date"`
			TotalBudget float64 `json:"totalBudget"`
		}
		var budgetRows []BudgetRow

		// Создаём отдельный запрос для бюджета с фильтром по статусу "завершён"
		budgetQuery := db.Table("orders").Where("status = ?", "завершён")
		if !startDate.IsZero() {
			budgetQuery = budgetQuery.Where("created_at >= ?", startDate)
		}
		if !endDate.IsZero() {
			budgetQuery = budgetQuery.Where("created_at <= ?", endDate)
		}
		if err := budgetQuery.Select("DATE(created_at) as date, SUM(budget) as total_budget").
			Group("DATE(created_at)").Order("DATE(created_at)").Scan(&budgetRows).Error; err != nil {
			log.Printf("AdminStatsAll: Ошибка подсчёта бюджета: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка подсчёта бюджета"})
			return
		}

		// Если по каким-либо причинам требуется агрегировать данные по датам дополнительно:
		budgetMap := make(map[string]float64)
		for _, row := range budgetRows {
			budgetMap[row.Date] += row.TotalBudget
		}
		budgetRows = nil
		for date, total := range budgetMap {
			budgetRows = append(budgetRows, BudgetRow{Date: date, TotalBudget: total})
		}
		sort.Slice(budgetRows, func(i, j int) bool {
			return budgetRows[i].Date < budgetRows[j].Date
		})
		log.Printf("AdminStatsAll: budgetRows: %+v", budgetRows)

		// 3) Количество новых заказов по дням – здесь оставляем все заказы без фильтра по статусу.
		type NewOrdersRow struct {
			Date  string `json:"date"`
			Count int    `json:"count"`
		}
		var newOrdersRows []NewOrdersRow
		if err := query.Select("DATE(created_at) as date, COUNT(*) as count").
			Group("DATE(created_at)").Order("DATE(created_at)").Scan(&newOrdersRows).Error; err != nil {
			log.Printf("AdminStatsAll: Ошибка подсчёта новых заказов: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка подсчёта новых заказов"})
			return
		}

		// Агрегируем новые заказы по датам
		newOrdersMap := make(map[string]int)
		for _, row := range newOrdersRows {
			newOrdersMap[row.Date] += row.Count
		}
		newOrdersRows = nil
		for date, count := range newOrdersMap {
			newOrdersRows = append(newOrdersRows, NewOrdersRow{Date: date, Count: count})
		}
		sort.Slice(newOrdersRows, func(i, j int) bool {
			return newOrdersRows[i].Date < newOrdersRows[j].Date
		})

		response := gin.H{
			"statusStats":    statusStats,
			"budgetStats":    budgetRows,
			"newOrdersStats": newOrdersRows,
		}
		log.Printf("AdminStatsAll: Ответ: %+v", response)
		c.JSON(http.StatusOK, response)
	}
}

// GetTodayEarnings возвращает сумму бюджета за текущий день для завершённых заказов.
func GetTodayEarnings(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		today := time.Now().Truncate(24 * time.Hour)
		var totalBudget float64

		err := db.Table("orders").
			Where("status = ?", "завершён").
			Select("COALESCE(SUM(budget), 0) as total_budget"). // Заменяем NULL на 0
			Where("DATE(created_at) = ?", today.Format("2006-01-02")).
			Scan(&totalBudget).Error

		if err != nil {
			log.Printf("GetTodayEarnings: Ошибка подсчёта бюджета за сегодня: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка подсчёта бюджета за сегодня"})
			return
		}

		response := gin.H{
			"totalBudget": totalBudget,
		}
		log.Printf("GetTodayEarnings: Ответ: %+v", response)
		c.JSON(http.StatusOK, response)
	}
}

// GetBudgetStats возвращает статистику бюджета по датам для завершённых заказов.
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

		// В этом запросе учитываются только завершённые заказы
		query := db.Model(&models.Order{}).
			Where("status = ?", "завершён").
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
			log.Printf("GetBudgetStats: Ошибка запроса к БД для статистики: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка запроса к статистике"})
			return
		}
		defer rows.Close()

		for rows.Next() {
			var dt time.Time
			var total float64
			if err := rows.Scan(&dt, &total); err != nil {
				log.Printf("GetBudgetStats: Ошибка чтения строки статистики: %v", err)
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
