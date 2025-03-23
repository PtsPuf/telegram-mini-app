package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

func main() {
	// Проверяем наличие переменных окружения
	if os.Getenv("TELEGRAM_BOT_TOKEN") == "" {
		// Если переменных нет, пробуем загрузить .env файл
		if err := godotenv.Load(); err != nil {
			log.Printf("Ошибка загрузки .env файла: %v", err)
		}
	}

	// Получаем порт из переменной окружения или используем значение по умолчанию
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Запускаем веб-сервер
	startServer(port)
}
