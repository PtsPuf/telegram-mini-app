// Package api provides the serverless functions for Vercel
package api

import (
	"net/http"
	"strings"

	"github.com/PtsPuf/telegram-mini-app/pkg/server"
)

// Handler is the entry point for Vercel serverless function
func Handler(w http.ResponseWriter, r *http.Request) {
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
}
