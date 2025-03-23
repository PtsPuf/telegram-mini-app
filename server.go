package main

import (
	"encoding/json"
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

	if r.Method != "POST" {
		log.Printf("Неверный метод запроса: %s", r.Method)
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var request struct {
		Question string   `json:"question"`
		Cards    []string `json:"cards"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		log.Printf("Ошибка декодирования запроса: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Printf("Получен POST запрос. Вопрос: %s, Карты: %v", request.Question, request.Cards)

	prediction, err := getPrediction(request.Question, request.Cards)
	if err != nil {
		log.Printf("Ошибка при получении предсказания: %v", err)
		http.Error(w, "Error generating prediction", http.StatusInternalServerError)
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
