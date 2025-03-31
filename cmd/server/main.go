// Package main для локального запуска сервера
package main

import (
	"log"
	"os"

	"github.com/PtsPuf/telegram-mini-app/pkg/server"
	"github.com/joho/godotenv"
)

func main() {
	log.Println("Запуск сервера локально через cmd/server...")

	// Попытка загрузить .env для локальной разработки
	if os.Getenv("OPENROUTER_API_KEY") == "" { // Проверяем одну из переменных
		log.Println("Переменные окружения не найдены, попытка загрузить .env файл...")
		if err := godotenv.Load(); err != nil {
			log.Printf("Предупреждение: Не удалось загрузить .env файл: %v", err)
		}
	}

	// Запускаем настройку и сервер напрямую
	server.SetupAndRunServer()

	log.Println("Локальный сервер запущен. Нажмите Ctrl+C для выхода.")
	// Блокируем main горутину, чтобы сервер продолжал работать в фоне
	select {}
}
