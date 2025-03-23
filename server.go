package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"
)

// UserState представляет состояние пользователя
type UserState struct {
	Name         string `json:"name"`
	BirthDate    string `json:"birthDate"`
	Question     string `json:"question"`
	Mode         string `json:"mode"`
	PartnerName  string `json:"partnerName"`
	PartnerBirth string `json:"partnerBirth"`
	Step         int    `json:"step"`
}

// WebAppState представляет состояние веб-приложения
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
	webAppStates       = make(map[string]*WebAppState)
	webAppMu           sync.Mutex
	OPENROUTER_API_KEY = os.Getenv("OPENROUTER_API_KEY")
)

func handleWebRequest(w http.ResponseWriter, r *http.Request) {
	log.Printf("=== Начало обработки запроса ===")
	log.Printf("Метод: %s", r.Method)
	log.Printf("URL: %s", r.URL.String())
	log.Printf("RemoteAddr: %s", r.RemoteAddr)
	log.Printf("Headers: %+v", r.Header)
	log.Printf("Content-Type: %s", r.Header.Get("Content-Type"))
	log.Printf("Content-Length: %s", r.Header.Get("Content-Length"))

	// Добавляем CORS заголовки
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, HEAD, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Max-Age", "3600")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	// Обработка preflight запроса
	if r.Method == "OPTIONS" {
		log.Printf("Обработка OPTIONS запроса")
		w.WriteHeader(http.StatusOK)
		log.Printf("=== Конец обработки OPTIONS запроса ===")
		return
	}

	// Обработка HEAD запроса
	if r.Method == "HEAD" {
		log.Printf("Обработка HEAD запроса")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		log.Printf("=== Конец обработки HEAD запроса ===")
		return
	}

	// Для GET запросов возвращаем статус сервера
	if r.Method == "GET" {
		log.Printf("Обработка GET запроса")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "Server is running",
		})
		log.Printf("=== Конец обработки GET запроса ===")
		return
	}

	if r.Method != http.MethodPost {
		log.Printf("Неверный метод запроса: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		log.Printf("=== Конец обработки - неверный метод ===")
		return
	}

	// Читаем тело запроса для логирования
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Ошибка чтения тела запроса: %v", err)
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		log.Printf("=== Конец обработки - ошибка чтения тела ===")
		return
	}
	log.Printf("Тело запроса: %s", string(body))

	// Восстанавливаем тело запроса для дальнейшего использования
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	var state UserState
	if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
		log.Printf("Ошибка декодирования JSON: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		log.Printf("=== Конец обработки - ошибка декодирования JSON ===")
		return
	}

	log.Printf("=== Данные запроса ===")
	log.Printf("Имя: %s", state.Name)
	log.Printf("Дата рождения: %s", state.BirthDate)
	log.Printf("Вопрос: %s", state.Question)
	log.Printf("Режим: %s", state.Mode)
	log.Printf("Имя партнера: %s", state.PartnerName)
	log.Printf("Дата рождения партнера: %s", state.PartnerBirth)
	log.Printf("Шаг: %d", state.Step)
	log.Printf("=== Конец данных запроса ===")

	log.Printf("Начинаем генерацию предсказания...")
	prediction, err := getPrediction(&state)
	if err != nil {
		log.Printf("Ошибка получения предсказания: %v", err)
		http.Error(w, fmt.Sprintf("Failed to generate prediction: %v", err), http.StatusInternalServerError)
		log.Printf("=== Конец обработки - ошибка генерации предсказания ===")
		return
	}

	log.Printf("Предсказание успешно сгенерировано: %s", prediction)

	response := map[string]string{
		"prediction": prediction,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Ошибка кодирования ответа: %v", err)
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
		log.Printf("=== Конец обработки - ошибка кодирования ответа ===")
		return
	}

	log.Printf("Ответ успешно отправлен")
	log.Printf("=== Конец обработки запроса ===")
}

func getPrediction(state *UserState) (string, error) {
	log.Printf("=== Начало генерации предсказания ===")
	log.Printf("Инициализация конфигурации OpenAI...")
	log.Printf("API Key: %s...", OPENROUTER_API_KEY[:4]) // Показываем только первые 4 символа ключа для безопасности

	config := openai.DefaultConfig(OPENROUTER_API_KEY)
	config.BaseURL = "https://openrouter.ai/api/v1"
	client := openai.NewClientWithConfig(config)
	log.Printf("Конфигурация OpenAI инициализирована")

	// Формируем промпт с учетом вопроса
	prompt := fmt.Sprintf(`Ты - опытная гадалка на картах Таро. Пользователь по имени %s, родившийся %s, задал вопрос: "%s"
Сфера вопроса: %s

Сделай предсказание, которое:
1. Напрямую отвечает на вопрос пользователя
2. Дает конкретные рекомендации
3. Учитывает сферу вопроса (%s)

Пиши живым, эмоциональным языком, но сохраняй профессионализм.`,
		state.Name, state.BirthDate, state.Question, state.Mode, state.Mode)

	if state.PartnerName != "" && state.PartnerBirth != "" {
		prompt += fmt.Sprintf("\n\nПартнер: %s, родился(ась) %s.", state.PartnerName, state.PartnerBirth)
	}

	log.Printf("Сформирован промпт: %s", prompt)
	log.Printf("Отправляем запрос к OpenAI...")
	log.Printf("Модель: google/gemma-3-27b-it:free")
	log.Printf("Temperature: 0.7")
	log.Printf("MaxTokens: 1000")

	resp, err := client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: "google/gemma-3-27b-it:free",
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "Ты - опытная гадалка на картах Таро с глубоким пониманием их значений и способностью давать точные и полезные предсказания.",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			Temperature: 0.7,
			MaxTokens:   1000,
		},
	)

	if err != nil {
		log.Printf("Ошибка при получении предсказания от OpenAI: %v", err)
		log.Printf("Тип ошибки: %T", err)
		if urlErr, ok := err.(*url.Error); ok {
			log.Printf("URL Error: %v", urlErr)
			log.Printf("Timeout: %v", urlErr.Timeout())
			log.Printf("Temporary: %v", urlErr.Temporary())
		}
		return "", fmt.Errorf("ошибка при получении предсказания: %v", err)
	}

	if len(resp.Choices) == 0 {
		log.Printf("Не получен ответ от модели OpenAI")
		log.Printf("Полный ответ: %+v", resp)
		return "", fmt.Errorf("не получен ответ от модели")
	}

	prediction := resp.Choices[0].Message.Content
	log.Printf("Получено предсказание: %s", prediction)
	log.Printf("Длина предсказания: %d символов", len(prediction))
	log.Printf("=== Конец генерации предсказания ===")
	return prediction, nil
}

func startServer(port string) {
	// Проверяем наличие API ключа
	if OPENROUTER_API_KEY == "" {
		log.Fatal("OPENROUTER_API_KEY не установлен в переменных окружения")
	}

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
