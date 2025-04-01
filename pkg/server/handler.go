// Package server provides HTTP server functionality for the telegram mini app
package server

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
	// States maps user IDs to their state
	States = make(map[string]*common.UserState)
	// StateMutex protects the States map
	StateMutex sync.RWMutex

	// serverInstance используется только для локального запуска
	serverInstance *http.Server
)

// NewMux создает и возвращает настроенный ServeMux
func NewMux() *http.ServeMux {
	mux := http.NewServeMux()

	// Serve static files - ИСПОЛЬЗУЕМ "static"
	fs := http.FileServer(http.Dir("static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))
	mux.Handle("/", fs) // Отдаем index.html и другие файлы из static

	// Handle prediction endpoint
	mux.HandleFunc("/prediction", HandlePrediction)

	return mux
}

// SetupAndRunServer initializes and starts the HTTP server *for local execution*
func SetupAndRunServer() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Setting up local server on port %s", port)

	mux := NewMux()

	serverInstance = &http.Server{
		Addr:         ":" + port,
		Handler:      AddHeaders(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 20 * time.Second,
	}

	log.Printf("Starting local server on port %s", port)
	go func() {
		if err := serverInstance.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start local server: %v", err)
		}
	}()
}

// Handler is the entry point for Vercel and other HTTP requests
func Handler(w http.ResponseWriter, r *http.Request) {
	// Создаем новый mux для каждого запроса в serverless среде
	mux := NewMux()

	// Применяем заголовки и обрабатываем запрос
	AddHeaders(mux).ServeHTTP(w, r)
}

// AddHeaders adds security and caching headers to all responses
func AddHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		log.Printf("[CORS Debug] Received Origin header: %s", origin)

		// Возвращаем проверку Origin + добавляем 'null'
		allowedOrigin := "https://ptspuf.github.io"
		isAllowed := false
		if origin == allowedOrigin || origin == "null" { // Разрешаем конкретный домен или null
			isAllowed = true
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			// Можно временно оставить '*', если вы тестируете с других источников
			// w.Header().Set("Access-Control-Allow-Origin", "*")
			log.Printf("[CORS Warning] Request origin '%s' is not allowed.", origin)
		}

		// Устанавливаем остальные CORS заголовки, ТОЛЬКО если источник разрешен
		if isAllowed {
			w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, HEAD")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept, Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Max-Age", "3600")
			w.Header().Add("Vary", "Origin") // Vary нужен только если Allow-Origin не всегда *
		} else if r.Method == "OPTIONS" {
			// Для неразрешенных источников на OPTIONS отвечаем без CORS заголовков,
			// чтобы браузер понял, что запрос не разрешен
			log.Printf("[CORS Debug] Handling preflight OPTIONS request from DISALLOWED origin: %s", origin)
			w.WriteHeader(http.StatusForbidden) // Или http.StatusOK без CORS заголовков
			return
		}

		// Handle preflight OPTIONS request (если источник был разрешен)
		if r.Method == "OPTIONS" && isAllowed {
			log.Printf("[CORS Debug] Handling preflight OPTIONS request from allowed origin: %s", origin)
			w.WriteHeader(http.StatusOK)
			return
		}

		// Если источник не разрешен и это не OPTIONS, прерываем обработку
		if !isAllowed && r.Method != "OPTIONS" {
			log.Printf("[CORS Error] Blocking request from disallowed origin: %s for path: %s", origin, r.URL.Path)
			http.Error(w, "CORS Origin Not Allowed", http.StatusForbidden)
			return
		}

		// ... (Установка остальных заголовков Security, Cache-Control) ...
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self' https://telegram.org; img-src 'self' data: https:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline' https://telegram.org;")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")

		// !!! ДОБАВЛЯЕМ ЛОГ ПЕРЕД ВЫЗОВОМ next.ServeHTTP !!!
		log.Printf("[AddHeaders Debug] About to call next.ServeHTTP for %s %s (Origin: %s, isAllowed: %t)", r.Method, r.URL.Path, origin, isAllowed)
		next.ServeHTTP(w, r)
	})
}

// HandlePrediction processes prediction requests
func HandlePrediction(w http.ResponseWriter, r *http.Request) {
	log.Printf("HandlePrediction: Получен запрос на /prediction")
	log.Printf("HandlePrediction: Метод запроса: %s", r.Method)
	// --- УБИРАЕМ ДУБЛИРУЮЩИЕСЯ CORS ЗАГОЛОВКИ ---
	/*
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, HEAD")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept, Origin")
		w.Header().Set("Access-Control-Max-Age", "3600")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	*/

	// Handle preflight OPTIONS request (ЭТО УЖЕ ДЕЛАЕТСЯ В AddHeaders, но оставим на всякий случай, хотя он не должен сюда дойти)
	if r.Method == "OPTIONS" {
		log.Printf("HandlePrediction: Обработка OPTIONS запроса (дублирующая проверка)")
		w.WriteHeader(http.StatusOK)
		return
	}

	// Handle HEAD request
	if r.Method == "HEAD" {
		// ... (код обработки HEAD)
		log.Printf("HandlePrediction: Обработка HEAD запроса")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		// ... (код обработки не POST)
		log.Printf("HandlePrediction: Неподдерживаемый метод: %s", r.Method)
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	// Устанавливаем ЗАГОЛОВКИ ОТВЕТА (не CORS)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	// ... (остальная логика чтения тела, вызова GetPrediction, отправки ответа) ...
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("HandlePrediction: Ошибка чтения тела запроса: %v", err)
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	log.Printf("HandlePrediction: Тело запроса: %s", string(body))

	var state common.UserState
	if err := json.Unmarshal(body, &state); err != nil {
		log.Printf("HandlePrediction: Ошибка декодирования JSON: %v", err)
		http.Error(w, "Invalid JSON in request body", http.StatusBadRequest)
		return
	}

	log.Printf("HandlePrediction: Получен запрос на предсказание для пользователя: %s", state.Name)
	log.Printf("HandlePrediction: Данные запроса: %+v", state)

	prediction, err := GetPrediction(&state)
	if err != nil {
		log.Printf("HandlePrediction: Ошибка получения предсказания: %v", err)
		http.Error(w, fmt.Sprintf("Error getting prediction: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("HandlePrediction: Сгенерировано предсказание для пользователя: %s", state.Name)

	// ... (генерация изображений)
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
		log.Printf("HandlePrediction: Ошибка кодирования ответа: %v", err)
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
		return
	}

	log.Printf("HandlePrediction: Отправка ответа пользователю: %s", state.Name)
	log.Printf("HandlePrediction: Размер ответа: %d байт", len(responseJSON))

	w.Write(responseJSON)
	log.Printf("HandlePrediction: Успешно отправлен ответ пользователю: %s", state.Name)
}

// GetPrediction generates a prediction based on user state
func GetPrediction(state *common.UserState) (*common.Prediction, error) {
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

	// Дополняем массив промптов, если их меньше трех
	for len(imagePrompts) < 3 {
		defaultPrompt := fmt.Sprintf("A mystical tarot card for %s, with abstract shapes in Kandinsky style, vibrant colors", state.Name)
		imagePrompts = append(imagePrompts, defaultPrompt)
	}

	// Возвращаем только первые три промпта, если их больше
	if len(imagePrompts) > 3 {
		imagePrompts = imagePrompts[:3]
	}

	log.Printf("Извлечено промптов для изображений: %d", len(imagePrompts))
	for i, p := range imagePrompts {
		log.Printf("Промпт %d: %s", i+1, p)
	}

	return &common.Prediction{
		Text:         strings.Join(textParts, "\n"),
		ImagePrompts: imagePrompts,
	}, nil
}
