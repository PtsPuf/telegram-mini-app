package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
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
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var data struct {
		Name         string `json:"name"`
		BirthDate    string `json:"birthDate"`
		Question     string `json:"question"`
		Mode         string `json:"mode"`
		PartnerName  string `json:"partnerName"`
		PartnerBirth string `json:"partnerBirth"`
		Step         int    `json:"step"`
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Ошибка чтения тела запроса: %v", err)
		http.Error(w, "Ошибка чтения запроса", http.StatusBadRequest)
		return
	}
	log.Printf("Тело запроса: %s", string(body))

	if err := json.Unmarshal(body, &data); err != nil {
		log.Printf("Ошибка декодирования JSON: %v", err)
		http.Error(w, "Ошибка формата данных", http.StatusBadRequest)
		return
	}

	log.Printf("Получены данные: %+v", data)

	state := &UserState{
		Name:         data.Name,
		BirthDate:    data.BirthDate,
		Question:     data.Question,
		Mode:         data.Mode,
		PartnerName:  data.PartnerName,
		PartnerBirth: data.PartnerBirth,
		Step:         data.Step,
	}

	// Получаем предсказание
	prediction, err := getPrediction(state)
	if err != nil {
		log.Printf("Ошибка получения предсказания: %v", err)
		http.Error(w, "Ошибка получения предсказания", http.StatusInternalServerError)
		return
	}

	// Разбиваем предсказание на части
	parts := strings.Split(prediction, "\n\n***\n\n")
	if len(parts) != 3 {
		// Если нет явного разделителя, разбиваем по абзацам
		paragraphs := strings.Split(prediction, "\n\n")
		parts = make([]string, 3)
		for i := 0; i < 3 && i < len(paragraphs); i++ {
			parts[i] = paragraphs[i]
		}
	}

	// Генерируем изображения для каждой части
	var images []string
	for _, part := range parts {
		imgPrompt := generateImagePrompt(part)
		imgData, err := generateKandinskyImage(imgPrompt)
		if err != nil {
			log.Printf("Ошибка генерации изображения: %v", err)
			continue
		}
		// Конвертируем изображение в base64
		imgBase64 := base64.StdEncoding.EncodeToString(imgData)
		images = append(images, imgBase64)
	}

	// Формируем ответ
	response := struct {
		Predictions []string `json:"predictions"`
		Images     []string `json:"images"`
	}{
		Predictions: parts,
		Images:     images,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Ошибка отправки ответа: %v", err)
		http.Error(w, "Ошибка отправки ответа", http.StatusInternalServerError)
		return
	}
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