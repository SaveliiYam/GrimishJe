# Этап сборки
FROM golang:1.22-alpine AS builder

# Обновляем индекс пакетов и устанавливаем git
RUN apk update && apk add --no-cache git

WORKDIR /app

# Копируем go.mod и go.sum для кеширования зависимостей
COPY go.mod go.sum ./
RUN go mod download

# Копируем все исходники проекта
COPY . .

# Собираем бинарник
RUN CGO_ENABLED=0 GOOS=linux go build -o app ./cmd/main.go

# Финальный образ на базе Alpine
FROM alpine:latest
WORKDIR /app

# Копируем файл .env
COPY --from=builder /app/.env ./.env

# Создаем папку для бинарника и копируем его
RUN mkdir bin
COPY --from=builder /app/app ./bin/app

# Копируем статические файлы
COPY --from=builder /app/static ./static

# Переходим в папку с бинарником
WORKDIR /app/bin

# Экспонируем порт
EXPOSE 8080

# Запускаем приложение
CMD ["./app"]