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
	"github.com/MoshKillaPit/GrimishJe/pkg/utils"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/gorm"
)

var wsHub *websocket.Hub

var (
	maxFilesPerOrder = 10
	maxFileSize      = int64(10 * 1024 * 1024)
	allowedExts      = map[string]bool{
		// Изображения
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		// Документы
		".pdf":  true,
		".doc":  true,
		".docx": true,
		// Презентации
		".ppt":  true,
		".pptx": true,
		// Excel
		".xls":  true,
		".xlsx": true,
	}
)

type Config struct {
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	MinioBucket    string
}

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

	useSecure := false
	if os.Getenv("MINIO_USE_SSL") == "true" {
		useSecure = true
	}

	minioClient, err := minio.New(cfg.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""),
		Secure: useSecure,
	})
	if err != nil {
		log.Printf("Ошибка подключения к MinIO: %v", err)
		return nil, "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exists, err := minioClient.BucketExists(ctx, bucketName)
	if err != nil {
		log.Printf("Ошибка проверки bucket %s: %v", bucketName, err)
		return nil, "", err
	}
	if !exists {
		err = minioClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
		if err != nil {
			log.Printf("Ошибка создания bucket %s: %v", bucketName, err)
			return nil, "", err
		}
		log.Printf("Bucket %s успешно создан", bucketName)
	}

	log.Printf("Успешно подключено к MinIO с bucket %s", bucketName)
	return minioClient, bucketName, nil
}

func SetWebSocketHub(hub *websocket.Hub) {
	wsHub = hub
}

func UploadFiles(db *gorm.DB, minioClient *minio.Client, bucketName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		log.Println("Начало обработки загрузки файла")
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

		externalIP, err := utils.GetExternalIP()
		if err != nil {
			log.Printf("Ошибка получения внешнего IP-адреса: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка получения ip адреса"})
			return
		}

		externalDomain := fmt.Sprintf("http://%s:9000", externalIP)

		uploadedFiles := make([]struct {
			URL          string `json:"url"`
			OriginalName string `json:"originalName"`
			UploadedBy   string `json:"uploadedBy"`
		}, 0)

		uploadedByForm := c.PostForm("uploaded_by")
		var uploader string = "user"
		prefix := fmt.Sprintf("user_%d_", orderID)
		if uploadedByForm == "admin" || user.IsAdmin {
			prefix = fmt.Sprintf("admin_%d_", orderID)
			uploader = "admin"
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

			log.Printf("Сохранение файла %s в MinIO", newFileName)
			_, err = minioClient.PutObject(ctx, bucketName, newFileName, src, file.Size, minio.PutObjectOptions{
				ContentType: file.Header.Get("Content-Type"),
				UserMetadata: map[string]string{
					"x-amz-acl": "public-read",
				},
			})
			src.Close()
			if err != nil {
				log.Printf("Ошибка загрузки файла %s в MinIO: %v", file.Filename, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка загрузки файла"})
				return
			}
			log.Printf("Файл %s успешно сохранён в MinIO", newFileName)

			presignedURL, err := minioClient.PresignedGetObject(ctx, bucketName, newFileName, time.Hour, nil)
			if err != nil {
				log.Printf("Ошибка генерации URL для файла %s: %v", newFileName, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка генерации ссылки для скачивания"})
				return
			}
			log.Printf("Создан presigned URL: %s", presignedURL.String())

			// Заменяем хост в URL
			finalURL := strings.Replace(presignedURL.String(), "http://minio:9000", externalDomain, 1)

			fileRecord := models.File{
				OrderID:      uint(orderID),
				OriginalName: originalName,
				URL:          newFileName,
				UploadedBy:   uploader,
			}
			if err := db.Create(&fileRecord).Error; err != nil {
				log.Printf("Ошибка сохранения файла %s в базе данных: %v", newFileName, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка сохранения метаданных файла"})
				return
			}
			log.Printf("Файл сохранён в базе данных: %+v", fileRecord)

			uploadedFiles = append(uploadedFiles, struct {
				URL          string `json:"url"`
				OriginalName string `json:"originalName"`
				UploadedBy   string `json:"uploadedBy"`
			}{
				URL:          finalURL,
				OriginalName: originalName,
				UploadedBy:   uploader,
			})

			fileUpdate := websocket.FileUpdatePayload{
				OrderID:      uint(orderID),
				Filename:     newFileName,
				OriginalName: originalName,
				UploadedBy:   uploader,
				URL:          finalURL,
			}
			websocket.BroadcastMessage(wsHub, fileUpdate)
			log.Printf("Отправлено WebSocket-уведомление о файле: %+v", fileUpdate)

			if uploader == "admin" {
				notification := websocket.ChatMessagePayload{
					OrderID:    uint(orderID),
					Sender:     "Администратор",
					Message:    "Администратор загрузил файл: " + originalName,
					CreatedAt:  time.Now().Unix(),
					UploadedBy: "admin",
				}

				chatMsg := models.ChatMessage{
					OrderID:   notification.OrderID,
					Sender:    notification.Sender,
					Message:   notification.Message,
					CreatedAt: time.Unix(notification.CreatedAt, 0),
				}
				if err := db.Create(&chatMsg).Error; err != nil {
					log.Printf("Ошибка сохранения уведомления чата для OrderID %d: %v", notification.OrderID, err)
				}

				websocket.BroadcastMessage(wsHub, notification)
			}
		}

		log.Printf("Успешно загружено %d файлов для заказа %d пользователем %d (админ: %v, uploaded_by: %s)",
			len(uploadedFiles), orderID, userID, isAdmin, uploader)
		c.JSON(http.StatusOK, gin.H{
			"message": "Файлы успешно загружены",
			"files":   uploadedFiles,
		})
	}
}

func DeleteFile(c *gin.Context, db *gorm.DB, minioClient *minio.Client, bucketName string) {
	session := sessions.Default(c)
	uid := session.Get("user_id")
	if uid == nil {
		log.Println("Неавторизованный доступ при удалении файла")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Необходимо авторизоваться"})
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
		c.JSON(http.StatusForbidden, gin.H{"error": "Только администратор может удалять файлы"})
		return
	}

	var request struct {
		FileID  string `json:"fileID"`
		OrderID string `json:"orderID"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		log.Printf("Ошибка парсинга JSON: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат запроса"})
		return
	}

	orderID, err := strconv.ParseUint(request.OrderID, 10, 64)
	if err != nil {
		log.Printf("Неверный формат orderID: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
		return
	}

	fileID, err := strconv.ParseUint(request.FileID, 10, 64)
	if err != nil {
		log.Printf("Неверный формат fileID: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный fileID"})
		return
	}

	var file models.File
	if err := db.First(&file, fileID).Error; err != nil {
		log.Printf("Файл с ID %d не найден: %v", fileID, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "Файл не найден"})
		return
	}
	if file.OrderID != uint(orderID) {
		log.Printf("Файл с ID %d не принадлежит заказу %d", fileID, orderID)
		c.JSON(http.StatusForbidden, gin.H{"error": "Нет доступа к этому файлу"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = minioClient.RemoveObject(ctx, bucketName, file.URL, minio.RemoveObjectOptions{})
	if err != nil {
		log.Printf("Ошибка удаления файла %s из MinIO: %v", file.URL, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка удаления файла из хранилища"})
		return
	}

	if err := db.Delete(&file).Error; err != nil {
		log.Printf("Ошибка удаления файла %d из базы данных: %v", fileID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка удаления метаданных файла"})
		return
	}

	fileDeletePayload := websocket.FileDeletePayload{
		OrderID: uint(orderID),
		FileID:  uint(fileID),
	}
	websocket.BroadcastMessage(wsHub, fileDeletePayload)
	log.Printf("Отправлено WebSocket-уведомление об удалении файла: %+v", fileDeletePayload)

	c.JSON(http.StatusOK, gin.H{"message": "Файл успешно удалён"})
}

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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	externalIP, err := utils.GetExternalIP()
	if err != nil {
		log.Printf("Ошибка получения внешнего IP-адреса: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка получения ip адреса"})
	}

	externalDomain := fmt.Sprintf("http://%s:9000", externalIP)

	for i, file := range files {
		presignedURL, err := minioClient.PresignedGetObject(ctx, bucketName, file.URL, 24*time.Hour, nil)
		if err != nil {
			log.Printf("Ошибка генерации URL для файла %s: %v", file.URL, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка генерации ссылки для скачивания"})
			return
		}

		fileResponses[i] = gin.H{
			"url":          strings.Replace(presignedURL.String(), "http://minio:9000", externalDomain, 1),
			"originalName": file.OriginalName,
			"uploadedBy":   file.UploadedBy,
		}
	}

	log.Printf("Успешно возвращён список файлов для заказа %d (всего: %d) пользователем %d (админ: %v)", orderID, len(files), userID, isAdmin)
	c.JSON(http.StatusOK, gin.H{"files": fileResponses})
}

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

	externalIP, err := utils.GetExternalIP()
	if err != nil {
		log.Printf("Ошибка получения внешнего IP-адреса: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка получения ip адреса"})
		return
	}

	externalDomain := fmt.Sprintf("http://%s:9000", externalIP)

	_, err = minioClient.PutObject(ctx, bucketName, newFileName, src, file.Size, minio.PutObjectOptions{
		ContentType: file.Header.Get("Content-Type"),
	})
	if err != nil {
		log.Printf("Ошибка загрузки итогового файла %s в MinIO: %v", file.Filename, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка загрузки итогового файла"})
		return
	}

	fileRecord := models.File{
		OrderID:      uint(orderID),
		OriginalName: file.Filename,
		URL:          newFileName,
		UploadedBy:   "admin",
	}
	if err := db.Create(&fileRecord).Error; err != nil {
		log.Printf("Ошибка сохранения итогового файла %s в базе данных: %v", newFileName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка сохранения метаданных файла"})
		return
	}

	presignedURL, err := minioClient.PresignedGetObject(ctx, bucketName, newFileName, 24*time.Hour, nil)
	if err != nil {
		log.Printf("Ошибка генерации URL для итогового файла %s: %v", newFileName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка генерации ссылки для скачивания"})
		return
	}

	// Заменяем хост в URL
	finalURL := strings.Replace(presignedURL.String(), "http://minio:9000", externalDomain, 1)

	fileUpdate := websocket.FileUpdatePayload{
		OrderID:      uint(orderID),
		Filename:     newFileName,
		OriginalName: file.Filename,
		UploadedBy:   "admin",
		URL:          finalURL,
	}
	websocket.BroadcastMessage(wsHub, fileUpdate)
	log.Printf("Отправлено WebSocket-уведомление о загрузке итогового файла: %+v", fileUpdate)

	notification := websocket.ChatMessagePayload{
		OrderID:    uint(orderID),
		Sender:     "admin",
		Message:    "Администратор загрузил итоговый файл: " + file.Filename,
		CreatedAt:  time.Now().Unix(),
		UploadedBy: "admin",
	}

	chatMsg := models.ChatMessage{
		OrderID:   notification.OrderID,
		Sender:    notification.Sender,
		Message:   notification.Message,
		CreatedAt: time.Unix(notification.CreatedAt, 0),
	}
	if err := db.Create(&chatMsg).Error; err != nil {
		log.Printf("Ошибка сохранения уведомления чата для итогового файла, OrderID %d: %v", notification.OrderID, err)
	}

	websocket.BroadcastMessage(wsHub, notification)
	log.Printf("Отправлено WebSocket-уведомление о загрузке итогового файла: %+v", fileUpdate)

	log.Printf("Успешно загружен итоговый файл для заказа %d пользователем %d (админ)", orderID, userID)
	c.JSON(http.StatusOK, gin.H{
		"message":      "Итоговый файл успешно загружен",
		"finalFileURL": finalURL,
	})
}
