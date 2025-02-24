package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var (
	minioClient *minio.Client
	bucketName  = "orders"
	// Разрешенные форматы файлов
	allowedExts = map[string]bool{
		".pdf":  true,
		".doc":  true,
		".docx": true,
		".jpg":  true,
		".jpeg": true,
		".png":  true,
	}
)

// InitMinio инициализирует подключение к MinIO и создает bucket, если он не существует.
func InitMinio() {
	var err error
	// Замените параметры на ваши (endpoint, accessKey, secretKey)
	minioClient, err = minio.New("localhost:9000", &minio.Options{
		Creds:  credentials.NewStaticV4("minioaccesskey", "miniosecretkey", ""),
		Secure: false,
	})
	if err != nil {
		log.Fatalln("Ошибка подключения к MinIO:", err)
	}

	ctx := context.Background()
	exists, err := minioClient.BucketExists(ctx, bucketName)
	if err != nil {
		log.Fatalln("Ошибка проверки bucket:", err)
	}
	if !exists {
		err = minioClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{Region: "us-east-1"})
		if err != nil {
			log.Fatalln("Ошибка создания bucket:", err)
		}
	}
}

// UploadFiles обрабатывает загрузку файлов в MinIO и возвращает URL загруженных файлов с исходными именами.
func UploadFiles() gin.HandlerFunc {
	return func(c *gin.Context) {
		orderID := c.PostForm("orderID")
		if orderID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "orderID не указан"})
			return
		}
		form, err := c.MultipartForm()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Ошибка получения файлов"})
			return
		}
		files := form.File["files"]
		if len(files) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Нет загруженных файлов"})
			return
		}
		ctx := context.Background()
		uploadedFiles := make([]struct {
			URL          string `json:"url"`
			OriginalName string `json:"original_name"`
		}, 0)
		for _, file := range files {
			ext := strings.ToLower(filepath.Ext(file.Filename))
			if !allowedExts[ext] {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Формат файла %s не поддерживается", ext)})
				return
			}
			// Сохраняем оригинальное имя файла и формируем имя для MinIO с минимальным префиксом
			originalName := file.Filename
			newFileName := fmt.Sprintf("%s_%s", orderID, originalName) // Упрощаем имя, убираем timestamp
			src, err := file.Open()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка открытия файла"})
				return
			}
			defer src.Close()
			// Загрузка файла в MinIO
			_, err = minioClient.PutObject(ctx, bucketName, newFileName, src, file.Size, minio.PutObjectOptions{
				ContentType:  file.Header.Get("Content-Type"),
				UserMetadata: map[string]string{"original_name": originalName}, // Сохраняем оригинальное имя в метаданных
			})
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка загрузки файла"})
				return
			}
			// Формирование URL для доступа к файлу
			fileURL := fmt.Sprintf("http://localhost:9000/%s/%s", bucketName, newFileName)
			uploadedFiles = append(uploadedFiles, struct {
				URL          string `json:"url"`
				OriginalName string `json:"original_name"`
			}{URL: fileURL, OriginalName: originalName})
		}
		c.JSON(http.StatusOK, gin.H{"message": "Файлы успешно загружены", "files": uploadedFiles})
	}
}

func ListFiles() gin.HandlerFunc {
	return func(c *gin.Context) {
		orderID := c.Query("orderID")
		if orderID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "orderID не указан"})
			return
		}
		// Формируем префикс, который использовался при загрузке файлов (например, "orderID_")
		prefix := orderID + "_"
		ctx := context.Background()
		objectCh := minioClient.ListObjects(ctx, bucketName, minio.ListObjectsOptions{
			Prefix:    prefix,
			Recursive: true,
		})
		files := make([]struct {
			URL          string `json:"url"`
			OriginalName string `json:"original_name"`
		}, 0)
		for object := range objectCh {
			if object.Err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": object.Err.Error()})
				return
			}
			// Получаем метаданные объекта через StatObject
			objInfo, err := minioClient.StatObject(ctx, bucketName, object.Key, minio.StatObjectOptions{})
			if err != nil {
				log.Printf("Ошибка получения метаданных для файла %s: %v", object.Key, err)
				continue
			}
			originalName, ok := objInfo.UserMetadata["original_name"]
			if !ok {
				// Если метаданные отсутствуют, извлекаем оригинальное имя из конца пути (после префикса orderID_)
				parts := strings.Split(object.Key, "_")
				if len(parts) > 1 {
					originalName = strings.Join(parts[1:], "_") // Берем все после orderID_
				} else {
					originalName = filepath.Base(object.Key)
				}
			}
			// Формируем URL
			fileURL := fmt.Sprintf("http://localhost:9000/%s/%s", bucketName, object.Key)
			files = append(files, struct {
				URL          string `json:"url"`
				OriginalName string `json:"original_name"`
			}{URL: fileURL, OriginalName: originalName})
		}
		c.JSON(http.StatusOK, gin.H{"files": files})
	}
}
