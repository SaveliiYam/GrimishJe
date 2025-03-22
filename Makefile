# переменный цыетов
YELLOW := \033[1;33m
GREEN := \033[1;32m
RESET := \033[0m

# Компилятор Go
GO := go

# Путь к пакету приложения
PACKAGE := ./cmd

define INFO
	@echo "$(GREEN)[INFO]: $(1)$(RESET)"
endef

define WARN
	@echo "$(YELLOW)[WARN]: $(1)$(RESET)"
endef

.PHONY: get
get:
	@$(call INFO, "Установка зависимостей...")
	@go mod tidy

.PHONY: build
build:
	@$(call INFO, "Сборка проекта...")
	$(GO) build -o myapp $(PACKAGE)

.PHONY: format
format:
	@$(call INFO, "Форматирование кода...")
	@go fmt ./...
	@$(call INFO, "Форматирование кода закончено")
	@echo "-------------------------------------"

.PHONY: vet
vet:
	@$(call INFO, "Проверка кода с помощью go vet...")
	@go vet ./...
	@$(call INFO, "Проверка кода с помощью go vet закончена")
	@echo "-------------------------------------"

.PHONY: lint
lint:
ifeq (,$(shell which golangci-lint))
	@$(call WARN, "golangci-lint не найден. Устанавливаю...")
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@$(call INFO, "Установка линтера завершена.")
	@echo "-------------------------------------"
endif
	@$(call INFO, "Запуск линтера...")
	@golangci-lint run
	@$(call INFO, "Линтер закончил работу")
	@echo "-------------------------------------"

.PHONY: wsl-lint
wsl-lint:
ifeq (,$(shell which golangci-lint))
	@echo "golangci-lint не найден. Устанавливаю..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Установка линтера завершена."
	@echo "-------------------------------------"
endif
	@$(call INFO, "Запуск WSL линтера...")
	@golangci-lint run --no-config --disable-all --enable wsl
	@$(call INFO, "WSL линтер закончил работу.")
	@$(call INFO, "-------------------------------------")


.PHONY: deep-lint
deep-lint:
ifeq (,$(shell which golangci-lint))
	@$(call WARN, "golangci-lint не найден. Устанавливаю...")
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@$(call INFO, "Установка линтера завершена.")
	@echo "-------------------------------------"
endif
	@$(call INFO, "Запуск глубокого линтера...")
	@golangci-lint run -v --config golangci.yml
	@$(call INFO, "Глубокий закончил работу")
	@echo "-------------------------------------"

.PHONY: test
test:
	@$(call INFO, "Запуск тестов...")
	@$(GO) test ./...

.PHONY: clean
clean:
	@$(call INFO, "Очистка...")
	@rm -f myapp

.PHONY: run
run:
	@$(call INFO, "Запуск приложения...")
	@$(GO) run $(PACKAGE)
	@echo "-------------------------------------"

.PHONY: cascade-lint
cascade-lint:
	@$(call INFO, "Запуск линтеров...")
	@echo "-------------------------------------"
	@make vet && make lint &&  make deep-lint

.PHONY: swag-fmt
swag-fmt:
	@$(call INFO, "Форматирование swagger комментариев...")
	@swag fmt -d ./internal
	@$(call INFO, "Форматирование swagger комментариев закончено.")

.PHONY: swag-init
swag-init:
	@$(call INFO, "Создание документации...")
	@swag init -o ./docs -d ./cmd,./ --parseDependency --parseInternal
	@$(call INFO, "Создание документации закончено.")

.PHONY: swag
swag:
	@$(call INFO, "Создание документации и форматирование swagger комментариев...")
	@make swag-fmt && make swag-init