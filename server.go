package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

type WebAppState struct {
	Name         string `json:"name"`
	BirthDate    string `json:"birthDate"`
	Question     string `json:"question"`
	Mode         string `json:"mode"`
	PartnerName  string `json:"partnerName"`
	PartnerBirth string `json:"partnerBirth"`
	Step         int    `json:"step"`
}

var (
	webAppStates = make(map[string]*WebAppState)
	webAppMu     sync.Mutex
)

func handleWebRequest(w http.ResponseWriter, r *http.Request) {
	log.Printf("Получен %s запрос от %s", r.Method, r.RemoteAddr)

	// Добавляем CORS заголовки
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		log.Printf("Обработка OPTIONS запроса")
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		log.Printf("Неверный метод запроса: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Читаем тело запроса для логирования
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Ошибка чтения тела запроса: %v", err)
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(body))
	log.Printf("Тело запроса: %s", string(body))

	var state UserState
	if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
		log.Printf("Ошибка декодирования JSON: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Printf("Получен POST запрос с данными: %+v", state)

	prediction, err := getPrediction(&state)
	if err != nil {
		log.Printf("Ошибка получения предсказания: %v", err)
		http.Error(w, fmt.Sprintf("Failed to generate prediction: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Предсказание успешно сгенерировано")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"prediction": prediction,
	})
}

func startServer(port string) {
	// Настраиваем маршруты
	http.HandleFunc("/", handleWebRequest)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Настраиваем таймауты и размеры
	server := &http.Server{
		Addr:           ":" + port,
		Handler:        nil,
		ReadTimeout:    120 * time.Second,
		WriteTimeout:   120 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1MB
	}

	log.Printf("Сервер запущен на порту %s", port)
	if err := server.ListenAndServe(); err != nil {
		log.Printf("Ошибка запуска сервера: %v", err)
	}
}
