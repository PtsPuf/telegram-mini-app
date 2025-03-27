package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PtsPuf/telegram-mini-app/pkg/common"
)

var (
	states = make(map[string]*common.UserState)
	mu     sync.RWMutex
)

func startServer(port string) {
	log.Printf("Инициализация сервера...")
	log.Printf("Проверка переменных окружения:")
	log.Printf("OPENROUTER_API_KEY: %v", os.Getenv("OPENROUTER_API_KEY") != "")
	log.Printf("KANDINSKY_API_KEY: %v", os.Getenv("KANDINSKY_API_KEY") != "")
	log.Printf("KANDINSKY_SECRET: %v", os.Getenv("KANDINSKY_SECRET") != "")
	log.Printf("KANDINSKY_URL: %v", os.Getenv("KANDINSKY_URL") != "")

	// Create a custom server with timeouts
	server := &http.Server{
		Addr:           ":" + port,
		Handler:        nil,
		ReadTimeout:    300 * time.Second, // 5 minutes
		WriteTimeout:   300 * time.Second, // 5 minutes
		MaxHeaderBytes: 1 << 20,           // 1MB
	}

	// Create a custom mux for routing
	mux := http.NewServeMux()

	// Serve static files with custom headers
	fs := http.FileServer(http.Dir("static"))
	mux.Handle("/static/", addHeaders(http.StripPrefix("/static/", fs)))
	mux.Handle("/", addHeaders(fs))

	// Handle prediction endpoint
	mux.HandleFunc("/prediction", handlePrediction)

	// Set the custom mux as the server's handler
	server.Handler = mux

	log.Printf("Сервер настроен и готов к запуску на порту %s", port)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Ошибка запуска сервера: %v", err)
	}
}

// addHeaders adds security and caching headers to all responses
func addHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self' https://telegram.org; img-src 'self' data: https:; style-src 'self' 'unsafe-inline'; script-src 'self' https://telegram.org;")

		// Set caching headers
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")

		next.ServeHTTP(w, r)
	})
}

func handlePrediction(w http.ResponseWriter, r *http.Request) {
	log.Printf("Получен запрос на /prediction")
	log.Printf("Метод запроса: %s", r.Method)
	log.Printf("Заголовки запроса: %v", r.Header)

	// Set CORS headers first
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, HEAD")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Access-Control-Max-Age", "3600")

	// Handle preflight OPTIONS request
	if r.Method == "OPTIONS" {
		log.Printf("Обработка OPTIONS запроса")
		w.WriteHeader(http.StatusOK)
		return
	}

	// Handle HEAD request
	if r.Method == "HEAD" {
		log.Printf("Обработка HEAD запроса")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		log.Printf("Неподдерживаемый метод: %s", r.Method)
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	// Set content type
	w.Header().Set("Content-Type", "application/json")

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Ошибка чтения тела запроса: %v", err)
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	log.Printf("Тело запроса: %s", string(body))

	var state common.UserState
	if err := json.Unmarshal(body, &state); err != nil {
		log.Printf("Ошибка декодирования JSON: %v", err)
		http.Error(w, "Invalid JSON in request body", http.StatusBadRequest)
		return
	}

	log.Printf("Получен запрос на предсказание для пользователя: %s", state.Name)
	log.Printf("Данные запроса: %+v", state)

	// Get prediction
	prediction, err := getPrediction(&state)
	if err != nil {
		log.Printf("Ошибка получения предсказания: %v", err)
		http.Error(w, fmt.Sprintf("Error getting prediction: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Сгенерировано предсказание для пользователя: %s", state.Name)

	// Generate images
	var wg sync.WaitGroup
	var imageErrors []error
	images := make([][]byte, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			log.Printf("Начало генерации изображения %d для пользователя: %s", index+1, state.Name)
			img, err := common.GenerateKandinskyImage(prediction.ImagePrompts[index])
			if err != nil {
				log.Printf("Ошибка генерации изображения %d: %v", index+1, err)
				imageErrors = append(imageErrors, fmt.Errorf("error generating image %d: %v", index+1, err))
				return
			}
			images[index] = img
			log.Printf("Успешно сгенерировано изображение %d для пользователя: %s", index+1, state.Name)
		}(i)
	}
	wg.Wait()

	if len(imageErrors) > 0 {
		errMsgs := make([]string, len(imageErrors))
		for i, err := range imageErrors {
			errMsgs[i] = err.Error()
		}
		log.Printf("Ошибки при генерации изображений: %v", errMsgs)
		http.Error(w, fmt.Sprintf("Error generating images: %s", strings.Join(errMsgs, "; ")), http.StatusInternalServerError)
		return
	}

	response := common.PredictionResponse{
		Text:    prediction.Text,
		Images:  images,
		Prompts: prediction.ImagePrompts,
	}

	responseJSON, err := json.Marshal(response)
	if err != nil {
		log.Printf("Ошибка кодирования ответа: %v", err)
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
		return
	}

	log.Printf("Отправка ответа пользователю: %s", state.Name)
	log.Printf("Размер ответа: %d байт", len(responseJSON))

	w.Write(responseJSON)
	log.Printf("Успешно отправлен ответ пользователю: %s", state.Name)
}

func getPrediction(state *common.UserState) (*common.Prediction, error) {
	log.Printf("Начинаем генерацию предсказания для пользователя %s", state.Name)

	prompt := fmt.Sprintf("Ты - опытный таролог и экстрасенс. Тебе нужно дать предсказание для человека по имени %s (родился(ась) %s). "+
		"Вопрос: %s (сфера: %s). "+
		"Дай подробное предсказание (минимум 2000 символов). В конце предсказания сгенерируй три отдельных промпта для генерации изображений, "+
		"каждый начни с новой строки и префиксом 'IMAGE_PROMPT:'. Каждый промпт должен быть на английском языке и содержать описание изображения в стиле Кандинского.",
		state.Name, state.BirthDate, state.Question, state.Mode)

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY не установлен")
	}

	log.Printf("API ключ OpenRouter доступен, длина: %d", len(apiKey))
	client := common.NewOpenAIClient(apiKey)

	response, err := client.CreateChatCompletion(prompt)
	if err != nil {
		log.Printf("Ошибка при вызове OpenRouter API: %v", err)
		return nil, fmt.Errorf("error creating chat completion: %v", err)
	}

	log.Printf("Получен ответ от OpenRouter API, длина: %d символов", len(response))
	log.Printf("Ответ API: %s", response)

	// Ищем промпты для изображений по префиксу
	var imagePrompts []string
	var textParts []string
	lines := strings.Split(response, "\n")

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "IMAGE_PROMPT:") {
			prompt := strings.TrimPrefix(trimmedLine, "IMAGE_PROMPT:")
			imagePrompts = append(imagePrompts, strings.TrimSpace(prompt))
		} else {
			textParts = append(textParts, line)
		}
	}

	text := strings.Join(textParts, "\n")

	// Если не нашли промпты, пробуем другой способ - ищем последние три абзаца
	if len(imagePrompts) < 3 {
		log.Printf("Не найдены промпты с префиксом, пробуем альтернативный метод")
		parts := strings.Split(response, "\n\n")
		if len(parts) >= 4 {
			text = strings.Join(parts[:len(parts)-3], "\n\n")
			imagePrompts = parts[len(parts)-3:]
		} else {
			// Если не удалось разделить, генерируем стандартные промпты
			log.Printf("Не удалось выделить промпты, используем стандартные")
			imagePrompts = []string{
				"Abstract spiritual energy in Kandinsky style with vibrant colors",
				"Mystical symbols and patterns in Kandinsky composition",
				"Cosmic harmony and balance in abstract Kandinsky expressionism",
			}
		}
	}

	// Проверяем количество промптов
	for len(imagePrompts) < 3 {
		log.Printf("Недостаточно промптов, добавляем стандартный")
		imagePrompts = append(imagePrompts, "Abstract spiritual energy in Kandinsky style")
	}

	// Обрезаем до трех промптов
	if len(imagePrompts) > 3 {
		imagePrompts = imagePrompts[:3]
	}

	log.Printf("Финальный текст предсказания, длина: %d символов", len(text))
	log.Printf("Промпты для изображений: %v", imagePrompts)

	return &common.Prediction{
		Text:         text,
		ImagePrompts: imagePrompts,
	}, nil
}
