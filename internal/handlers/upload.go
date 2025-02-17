package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

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

// UploadFiles обрабатывает загрузку файлов в MinIO и возвращает URL загруженных файлов.
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
		uploadedFiles := []string{}
		for _, file := range files {
			ext := strings.ToLower(filepath.Ext(file.Filename))
			if !allowedExts[ext] {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Формат файла %s не поддерживается", ext)})
				return
			}
			// Формирование уникального имени файла: orderID_timestamp_originalname
			newFileName := fmt.Sprintf("%s_%d_%s", orderID, time.Now().UnixNano(), file.Filename)
			src, err := file.Open()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка открытия файла"})
				return
			}
			defer src.Close()
			// Загрузка файла в MinIO
			_, err = minioClient.PutObject(ctx, bucketName, newFileName, src, file.Size, minio.PutObjectOptions{
				ContentType: file.Header.Get("Content-Type"),
			})
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка загрузки файла"})
				return
			}
			// Формирование URL для доступа к файлу (настройте под свои условия)
			fileURL := fmt.Sprintf("http://%s/%s/%s", "localhost:9000", bucketName, newFileName)
			uploadedFiles = append(uploadedFiles, fileURL)
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
		files := []string{}
		for object := range objectCh {
			if object.Err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": object.Err.Error()})
				return
			}
			// Формируем URL. Если ваше приложение работает в Docker, возможно, потребуется использовать другое имя хоста.
			fileURL := fmt.Sprintf("http://localhost:9000/%s/%s", bucketName, object.Key)
			files = append(files, fileURL)
		}
		c.JSON(http.StatusOK, gin.H{"files": files})
	}
}
