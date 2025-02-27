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
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/gorm"
)

var (
	minioClient      *minio.Client
	bucketName       string
	maxFilesPerOrder = 10                      // Ограничение на максимальное количество файлов на заказ
	maxFileSize      = int64(10 * 1024 * 1024) // 10MB максимальный размер файла
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
func InitMinio(db *gorm.DB, cfg *Config) error {
	if err := godotenv.Load(); err != nil {
		log.Println("Не удалось загрузить .env файл, используются переменные окружения")
	}

	// Загружаем конфигурацию из .env или используем значения по умолчанию
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
			cfg.MinioBucket = "orders" // Устанавливаем значение по умолчанию вручную
		}
	}

	bucketName = cfg.MinioBucket

	// Подключаемся к MinIO
	var err error
	minioClient, err = minio.New(cfg.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""),
		Secure: false,
	})
	if err != nil {
		log.Fatalf("Ошибка подключения к MinIO: %v", err)
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exists, err := minioClient.BucketExists(ctx, bucketName)
	if err != nil {
		log.Fatalf("Ошибка проверки bucket %s: %v", bucketName, err)
		return err
	}
	if !exists {
		err = minioClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{Region: "us-east-1"})
		if err != nil {
			log.Fatalf("Ошибка создания bucket %s: %v", bucketName, err)
			return err
		}
		log.Printf("Bucket %s успешно создан", bucketName)
	}

	log.Printf("Успешно подключено к MinIO с bucket %s", bucketName)
	return nil
}

// UploadFiles обрабатывает загрузку файлов в MinIO для конкретного заказа.
func UploadFiles(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Проверка авторизации
		session := sessions.Default(c)
		uid := session.Get("user_id")
		if uid == nil {
			log.Println("Неавторизованный доступ при загрузке файлов")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Необходимо авторизоваться"})
			return
		}

		orderIDStr := c.PostForm("orderID")
		if orderIDStr == "" {
			log.Println("Отсутствует orderID в запросе на загрузку файлов")
			c.JSON(http.StatusBadRequest, gin.H{"error": "orderID не указан"})
			return
		}

		orderID, err := strconv.ParseUint(orderIDStr, 10, 64)
		if err != nil {
			log.Printf("Неверный формат orderID: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный orderID"})
			return
		}

		// Проверка принадлежности заказа пользователю или роли администратора
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

		// Проверяем, является ли пользователь администратором
		var user models.User
		if err := db.First(&user, userID).Error; err != nil {
			log.Printf("Пользователь с ID %d не найден: %v", userID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Пользователь не найден"})
			return
		}
		isAdmin = user.IsAdmin // Предполагается, что в модели User есть поле IsAdmin

		// Проверка прав доступа: либо пользователь является владельцем заказа, либо администратором
		if order.UserID != userID && !isAdmin {
			log.Printf("Пользователь %d (не администратор) попытался загрузить файлы для чужого заказа %d", userID, orderID)
			c.JSON(http.StatusForbidden, gin.H{"error": "Нет доступа к этому заказу"})
			return
		}

		// Получаем файлы из формы
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

		// Проверяем лимит файлов (не более 10)
		if len(files) > maxFilesPerOrder {
			log.Printf("Превышен лимит файлов (%d) для заказа %d", len(files), orderID)
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Можно загрузить не более %d файлов за раз", maxFilesPerOrder)})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		uploadedFiles := make([]struct {
			URL          string `json:"url"`
			OriginalName string `json:"original_name"`
		}, 0)

		prefix := fmt.Sprintf("%d_", orderID)
		for _, file := range files {
			// Проверка размера файла (10MB)
			if file.Size > maxFileSize {
				log.Printf("Файл %s превышает максимальный размер (%d байт) для заказа %d", file.Filename, maxFileSize, orderID)
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Файл %s превышает максимальный размер (%d байт)", file.Filename, maxFileSize)})
				return
			}

			// Проверка расширения файла
			ext := strings.ToLower(filepath.Ext(file.Filename))
			if !allowedExts[ext] {
				log.Printf("Неподдерживаемый формат файла %s для заказа %d", file.Filename, orderID)
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Формат файла %s не поддерживается", ext)})
				return
			}

			originalName := file.Filename
			newFileName := fmt.Sprintf("%s%s", prefix, originalName) // Уникальное имя с префиксом orderID

			// Открываем файл
			src, err := file.Open()
			if err != nil {
				log.Printf("Ошибка открытия файла %s: %v", file.Filename, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка открытия файла"})
				return
			}
			defer src.Close()

			// Загружаем файл в MinIO
			_, err = minioClient.PutObject(ctx, bucketName, newFileName, src, file.Size, minio.PutObjectOptions{
				ContentType: file.Header.Get("Content-Type"),
				UserMetadata: map[string]string{
					"original_name": originalName, // Сохраняем оригинальное имя файла в метаданных
				},
			})
			if err != nil {
				log.Printf("Ошибка загрузки файла %s в MinIO: %v", file.Filename, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка загрузки файла"})
				return
			}

			// Генерируем URL для доступа к файлу
			presignedURL, err := minioClient.PresignedGetObject(ctx, bucketName, newFileName, time.Hour, nil)
			if err != nil {
				log.Printf("Ошибка генерации URL для файла %s: %v", newFileName, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка генерации ссылки для скачивания"})
				return
			}

			uploadedFiles = append(uploadedFiles, struct {
				URL          string `json:"url"`
				OriginalName string `json:"original_name"`
			}{
				URL:          presignedURL.String(),
				OriginalName: originalName,
			})
		}

		log.Printf("Успешно загружено %d файлов для заказа %d пользователем %d (админ: %v)", len(uploadedFiles), orderID, userID, isAdmin)
		c.JSON(http.StatusOK, gin.H{
			"message": "Файлы успешно загружены",
			"files":   uploadedFiles,
		})
	}
}

// ListFiles возвращает список файлов, связанных с заказом.
func ListFiles(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Проверка авторизации
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

		// Проверка принадлежности заказа пользователю или роли администратора
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

		// Проверяем, является ли пользователь администратором
		var user models.User
		if err := db.First(&user, userID).Error; err != nil {
			log.Printf("Пользователь с ID %d не найден: %v", userID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Пользователь не найден"})
			return
		}
		isAdmin = user.IsAdmin // Предполагается, что в модели User есть поле IsAdmin

		// Проверка прав доступа: либо пользователь является владельцем заказа, либо администратором
		if order.UserID != userID && !isAdmin {
			log.Printf("Пользователь %d (не администратор) попытался получить файлы чужого заказа %d", userID, orderID)
			c.JSON(http.StatusForbidden, gin.H{"error": "Нет доступа к этому заказу"})
			return
		}

		// Получаем список файлов из MinIO
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		prefix := fmt.Sprintf("%d_", orderID)
		files := make([]struct {
			URL          string `json:"url"`
			OriginalName string `json:"original_name"`
		}, 0)

		objects := minioClient.ListObjects(ctx, bucketName, minio.ListObjectsOptions{
			Prefix:    prefix,
			Recursive: true,
		})
		for object := range objects {
			if object.Err != nil {
				log.Printf("Ошибка при перечислении объектов для заказа %d: %v", orderID, object.Err)
				continue
			}

			// Получаем метаданные объекта
			objInfo, err := minioClient.StatObject(ctx, bucketName, object.Key, minio.StatObjectOptions{})
			if err != nil {
				log.Printf("Ошибка получения метаданных для файла %s: %v", object.Key, err)
				continue
			}

			originalName, ok := objInfo.UserMetadata["original_name"]
			if !ok {
				// Если метаданные отсутствуют, извлекаем имя из ключа, убирая префикс
				fileName := strings.TrimPrefix(object.Key, prefix)
				if fileName == "" {
					continue
				}
				originalName = fileName
			}

			// Генерируем URL для доступа к файлу
			presignedURL, err := minioClient.PresignedGetObject(ctx, bucketName, object.Key, time.Hour, nil)
			if err != nil {
				log.Printf("Ошибка генерации URL для файла %s: %v", object.Key, err)
				continue
			}

			files = append(files, struct {
				URL          string `json:"url"`
				OriginalName string `json:"original_name"`
			}{
				URL:          presignedURL.String(),
				OriginalName: originalName,
			})
		}

		log.Printf("Успешно возвращён список файлов для заказа %d (всего: %d) пользователем %d (админ: %v)", orderID, len(files), userID, isAdmin)
		c.JSON(http.StatusOK, gin.H{"files": files})
	}
}
