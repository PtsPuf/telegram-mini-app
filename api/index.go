// Package api provides the serverless functions for Vercel
package api

import (
	"fmt"
	"log"
	"net/http"
	// Убираем ненужные импорты для этого теста
	// "strings"
	// "github.com/PtsPuf/telegram-mini-app/pkg/server"
)

// Handler is the entry point for Vercel serverless function
func Handler(w http.ResponseWriter, r *http.Request) {
	// --- ОТЛАДОЧНЫЙ КОД ---
	log.Printf("[DEBUG Vercel Handler] Received request. Method: %s, Path: %s, URL: %s", r.Method, r.URL.Path, r.URL.String())

	// Устанавливаем CORS заголовки, чтобы браузер не ругался на ответ
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, HEAD")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept, Origin")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Max-Age", "3600")

	// Специально обрабатываем OPTIONS для preflight запросов
	if r.Method == "OPTIONS" {
		log.Println("[DEBUG Vercel Handler] Responding OK to OPTIONS request.")
		w.WriteHeader(http.StatusOK)
		return
	}

	// Просто возвращаем 200 OK для всех остальных запросов (включая POST /api/prediction)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "API Handler Reached Successfully")
	log.Printf("[DEBUG Vercel Handler] Responded 200 OK to %s request for %s", r.Method, r.URL.Path)
	// --- КОНЕЦ ОТЛАДОЧНОГО КОДА ---

	/* --- Старый код закомментирован ---
	// Если запрос направлен на /api/prediction, перенаправляем на /prediction
	if strings.HasPrefix(r.URL.Path, "/api/prediction") {
		r.URL.Path = "/prediction"
	}

	// Добавляем CORS заголовки
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, HEAD")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Max-Age", "3600")

	// Делегируем обработку запроса нашему серверу
	server.Handler(w, r)
	*/
}
