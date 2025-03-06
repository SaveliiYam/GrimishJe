package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MoshKillaPit/GrimishJe/internal/models"
	"github.com/MoshKillaPit/GrimishJe/internal/websocket"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/gorm"
)

// Глобальная переменная для WebSocket-хаба
var wsHub *websocket.Hub

var (
	maxFilesPerOrder = 10
	maxFileSize      = int64(10 * 1024 * 1024)
	allowedExts      = map[string]bool{
		".pdf":  true,
		".doc":  true,
		".docx": true,
		".jpg":  true,
		".jpeg": true,
		".png":  true,
	}
)

// Config хранит конфигурацию MinIO.
type Config struct {
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	MinioBucket    string
}

// InitMinio инициализирует подключение к MinIO и создаёт bucket, если он не существует.
func InitMinio(db *gorm.DB, cfg *Config) (*minio.Client, string, error) {
	if cfg.MinioEndpoint == "" {
		cfg.MinioEndpoint = os.Getenv("MINIO_ENDPOINT")
	}
	if cfg.MinioAccessKey == "" {
		cfg.MinioAccessKey = os.Getenv("MINIO_ACCESS_KEY")
	}
	if cfg.MinioSecretKey == "" {
		cfg.MinioSecretKey = os.Getenv("MINIO_SECRET_KEY")
	}
	if cfg.MinioBucket == "" {
		cfg.MinioBucket = os.Getenv("MINIO_BUCKET")
		if cfg.MinioBucket == "" {
			cfg.MinioBucket = "orders"
		}
	}

	bucketName := cfg.MinioBucket

	minioClient, err := minio.New(cfg.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""),
		Secure: cfg.MinioEndpoint != "localhost:9000", // HTTPS только для не локальных эндпоинтов
	})
	if err != nil {
		log.Fatalf("Ошибка подключения к MinIO: %v", err)
		return nil, "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exists, err := minioClient.BucketExists(ctx, bucketName)
	if err != nil {
		log.Fatalf("Ошибка проверки bucket %s: %v", bucketName, err)
		return nil, "", err
	}
	if !exists {
		err = minioClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
		if err != nil {
			log.Fatalf("Ошибка создания bucket %s: %v", bucketName, err)
			return nil, "", err
		}
		log.Printf("Bucket %s успешно создан", bucketName)
	}

	log.Printf("Успешно подключено к MinIO с bucket %s", bucketName)
	return minioClient, bucketName, nil
}

// SetWebSocketHub устанавливает глобальную переменную wsHub
func SetWebSocketHub(hub *websocket.Hub) {
	wsHub = hub
}

// UploadFiles обрабатывает загрузку файлов в MinIO для конкретного заказа с использованием префикса роли.
func UploadFiles(c *gin.Context, db *gorm.DB, minioClient *minio.Client, bucketName string) {
	if minioClient == nil {
		log.Println("MinIO клиент не инициализирован")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "MinIO клиент не инициализирован"})
		return
	}

	session := sessions.Default(c)
	uid := session.Get("user_id")
	if uid == nil {
		log.Println("Неавторизованный доступ при загрузке файлов")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Необходимо авторизоваться"})
		return
	}

	orderIDStr := c.PostForm("orderID")
	if orderIDStr == "" {
		log.Println("Отсутствует orderID в запросе на загрузке файлов")
		c.JSON(http.StatusBadRequest, gin.H{"error": "orderID не указан"})
		return
	}

	orderID, err := strconv.ParseUint(orderIDStr, 10, 64)
	if err != nil {
		log.Printf("Неверный формат orderID: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
		return
	}

	var order models.Order
	if err := db.First(&order, uint(orderID)).Error; err != nil {
		log.Printf("Заказ с ID %d не найден: %v", orderID, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
		return
	}

	var userID uint
	var isAdmin bool
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
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Невалидный user_id"})
			return
		}
		userID = uint(parsed)
	default:
		log.Println("Неподдерживаемый тип user_id в сессии")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Невалидный user_id"})
		return
	}

	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		log.Printf("Пользователь с ID %d не найден: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Пользователь не найден"})
		return
	}
	isAdmin = user.IsAdmin

	if order.UserID != userID && !isAdmin {
		log.Printf("Пользователь %d (не администратор) попытался загрузить файлы для чужого заказа %d", userID, orderID)
		c.JSON(http.StatusForbidden, gin.H{"error": "Нет доступа к этому заказу"})
		return
	}

	form, err := c.MultipartForm()
	if err != nil {
		log.Printf("Ошибка получения файлов: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ошибка получения файлов"})
		return
	}

	files := form.File["files"]
	if len(files) == 0 {
		log.Println("Нет загруженных файлов в запросе")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Нет загруженных файлов"})
		return
	}

	if len(files) > maxFilesPerOrder {
		log.Printf("Превышен лимит файлов (%d) для заказа %d", len(files), orderID)
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Можно загрузить не более %d файлов за раз", maxFilesPerOrder)})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	uploadedFiles := make([]struct {
		URL          string `json:"url"`
		OriginalName string `json:"originalName"`
		UploadedBy   string `json:"uploadedBy"`
	}, 0)

	prefix := fmt.Sprintf("user_%d_", orderID)
	if isAdmin {
		prefix = fmt.Sprintf("admin_%d_", orderID)
		log.Printf("Администратор (ID: %d) загружает файлы с префиксом '%s'", userID, prefix)
	}

	for _, file := range files {
		if file.Size > maxFileSize {
			log.Printf("Файл %s превышает максимальный размер (%d байт) для заказа %d", file.Filename, maxFileSize, orderID)
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Файл %s превышает максимальный размер (%d байт)", file.Filename, maxFileSize)})
			return
		}

		ext := strings.ToLower(filepath.Ext(file.Filename))
		if !allowedExts[ext] {
			log.Printf("Неподдерживаемый формат файла %s для заказа %d", file.Filename, orderID)
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Формат файла %s не поддерживается", ext)})
			return
		}

		originalName := file.Filename
		newFileName := fmt.Sprintf("%s%d_%s", prefix, time.Now().UnixNano(), originalName)

		src, err := file.Open()
		if err != nil {
			log.Printf("Ошибка открытия файла %s: %v", file.Filename, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка открытия файла"})
			return
		}
		defer src.Close()

		_, err = minioClient.PutObject(ctx, bucketName, newFileName, src, file.Size, minio.PutObjectOptions{
			ContentType: file.Header.Get("Content-Type"),
		})
		if err != nil {
			log.Printf("Ошибка загрузки файла %s в MinIO: %v", file.Filename, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка загрузки файла"})
			return
		}

		// Генерация безопасного presigned URL с подписью
		presignedURL, err := minioClient.PresignedGetObject(ctx, bucketName, newFileName, time.Hour, nil)
		if err != nil {
			log.Printf("Ошибка генерации URL для файла %s: %v", newFileName, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка генерации ссылки для скачивания"})
			return
		}

		uploadedBy := "user"
		if strings.HasPrefix(prefix, "admin_") {
			uploadedBy = "admin"
		}

		fileRecord := models.File{
			OrderID:      uint(orderID),
			OriginalName: originalName,
			URL:          presignedURL.String(),
			UploadedBy:   uploadedBy,
		}
		if err := db.Create(&fileRecord).Error; err != nil {
			log.Printf("Ошибка сохранения файла %s в базе данных: %v", newFileName, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка сохранения метаданных файла"})
			return
		}

		uploadedFiles = append(uploadedFiles, struct {
			URL          string `json:"url"`
			OriginalName string `json:"originalName"`
			UploadedBy   string `json:"uploadedBy"`
		}{
			URL:          presignedURL.String(),
			OriginalName: originalName,
			UploadedBy:   uploadedBy,
		})

		// WebSocket-уведомление
		fileUpdate := websocket.FileUpdatePayload{
			OrderID:      uint(orderID),
			Filename:     newFileName,
			OriginalName: originalName,
			URL:          presignedURL.String(),
		}
		websocket.BroadcastMessage(wsHub, fileUpdate)
		log.Printf("Отправлено WebSocket-уведомление о файле: %+v", fileUpdate)
	}

	log.Printf("Успешно загружено %d файлов для заказа %d пользователем %d (админ: %v, uploaded_by: %s)", len(uploadedFiles), orderID, userID, isAdmin)
	c.JSON(http.StatusOK, gin.H{
		"message": "Файлы успешно загружены",
		"files":   uploadedFiles,
	})
}

// ListFiles возвращает список файлов, связанных с заказом, из базы данных.
func ListFiles(c *gin.Context, db *gorm.DB, minioClient *minio.Client, bucketName string) {
	if minioClient == nil {
		log.Println("MinIO клиент не инициализирован")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "MinIO клиент не инициализирован"})
		return
	}

	session := sessions.Default(c)
	uid := session.Get("user_id")
	if uid == nil {
		log.Println("Неавторизованный доступ при запросе списка файлов")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Необходимо авторизоваться"})
		return
	}

	orderIDStr := c.Query("orderID")
	if orderIDStr == "" {
		log.Println("Отсутствует orderID в запросе списка файлов")
		c.JSON(http.StatusBadRequest, gin.H{"error": "orderID не указан"})
		return
	}

	orderID, err := strconv.ParseUint(orderIDStr, 10, 64)
	if err != nil {
		log.Printf("Неверный формат orderID: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
		return
	}

	var order models.Order
	if err := db.First(&order, uint(orderID)).Error; err != nil {
		log.Printf("Заказ с ID %d не найден: %v", orderID, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
		return
	}

	var userID uint
	var isAdmin bool
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
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Невалидный user_id"})
			return
		}
		userID = uint(parsed)
	default:
		log.Println("Неподдерживаемый тип user_id в сессии")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Невалидный user_id"})
		return
	}

	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		log.Printf("Пользователь с ID %d не найден: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Пользователь не найден"})
		return
	}
	isAdmin = user.IsAdmin

	if order.UserID != userID && !isAdmin {
		log.Printf("Пользователь %d (не администратор) попытался получить файлы чужого заказа %d", userID, orderID)
		c.JSON(http.StatusForbidden, gin.H{"error": "Нет доступа к этому заказу"})
		return
	}

	var files []models.File
	if err := db.Where("order_id = ?", uint(orderID)).Find(&files).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusOK, gin.H{"files": []gin.H{}})
			return
		}
		log.Printf("Ошибка получения файлов для заказа %d: %v", orderID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка получения файлов"})
		return
	}

	fileResponses := make([]gin.H, len(files))
	for i, file := range files {
		fileResponses[i] = gin.H{
			"url":          file.URL,
			"originalName": file.OriginalName,
			"uploadedBy":   file.UploadedBy,
		}
	}

	log.Printf("Успешно возвращён список файлов для заказа %d (всего: %d) пользователем %d (админ: %v)", orderID, len(files), userID, isAdmin)
	c.JSON(http.StatusOK, gin.H{"files": fileResponses})
}

// UploadFinalFile обрабатывает загрузку итогового файла администратором.
func UploadFinalFile(c *gin.Context, db *gorm.DB, minioClient *minio.Client, bucketName string) {
	if minioClient == nil {
		log.Println("MinIO клиент не инициализирован")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "MinIO клиент не инициализирован"})
		return
	}

	session := sessions.Default(c)
	uid := session.Get("user_id")
	if uid == nil {
		log.Println("Неавторизованный доступ при загрузке итогового файла")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Необходимо авторизоваться"})
		return
	}

	orderIDStr := c.PostForm("orderID")
	if orderIDStr == "" {
		log.Println("Отсутствует orderID в запросе на загрузку итогового файла")
		c.JSON(http.StatusBadRequest, gin.H{"error": "orderID не указан"})
		return
	}

	orderID, err := strconv.ParseUint(orderIDStr, 10, 64)
	if err != nil {
		log.Printf("Неверный формат orderID: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
		return
	}

	var order models.Order
	if err := db.First(&order, uint(orderID)).Error; err != nil {
		log.Printf("Заказ с ID %d не найден: %v", orderID, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "Заказ не найден"})
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
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Невалидный user_id"})
			return
		}
		userID = uint(parsed)
	default:
		log.Println("Неподдерживаемый тип user_id в сессии")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Невалидный user_id"})
		return
	}

	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		log.Printf("Пользователь с ID %d не найден: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Пользователь не найден"})
		return
	}
	if !user.IsAdmin {
		log.Printf("Пользователь %d не является администратором", userID)
		c.JSON(http.StatusForbidden, gin.H{"error": "Только администратор может загружать итоговый файл"})
		return
	}

	file, err := c.FormFile("finalFile")
	if err != nil {
		log.Printf("Ошибка получения итогового файла: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Нет итогового файла или ошибка получения"})
		return
	}

	if file.Size > maxFileSize {
		log.Printf("Итоговый файл %s превышает максимальный размер (%d байт)", file.Filename, maxFileSize)
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Файл %s превышает максимальный размер (%d байт)", file.Filename, maxFileSize)})
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if !allowedExts[ext] {
		log.Printf("Неподдерживаемый формат итогового файла %s", file.Filename)
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Формат файла %s не поддерживается", ext)})
		return
	}

	prefix := fmt.Sprintf("final_%d_", orderID)
	newFileName := fmt.Sprintf("%s%d_%s", prefix, time.Now().UnixNano(), file.Filename)

	src, err := file.Open()
	if err != nil {
		log.Printf("Ошибка открытия итогового файла %s: %v", file.Filename, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка открытия файла"})
		return
	}
	defer src.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = minioClient.PutObject(ctx, bucketName, newFileName, src, file.Size, minio.PutObjectOptions{
		ContentType: file.Header.Get("Content-Type"),
	})
	if err != nil {
		log.Printf("Ошибка загрузки итогового файла %s в MinIO: %v", file.Filename, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка загрузки итогового файла"})
		return
	}

	presignedURL, err := minioClient.PresignedGetObject(ctx, bucketName, newFileName, time.Hour, nil)
	if err != nil {
		log.Printf("Ошибка генерации URL для итогового файла %s: %v", newFileName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка генерации ссылки для скачивания"})
		return
	}

	fileRecord := models.File{
		OrderID:      uint(orderID),
		OriginalName: file.Filename,
		URL:          presignedURL.String(),
		UploadedBy:   "admin",
	}
	if err := db.Create(&fileRecord).Error; err != nil {
		log.Printf("Ошибка сохранения итогового файла %s в базе данных: %v", newFileName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка сохранения метаданных файла"})
		return
	}

	log.Printf("Успешно загружен итоговый файл для заказа %d пользователем %d (админ)", orderID, userID)
	c.JSON(http.StatusOK, gin.H{
		"message":      "Итоговый файл успешно загружен",
		"finalFileURL": presignedURL.String(),
	})
}
