package main

import (
	"encoding/json"
	"fmt"
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

	log.Printf("Starting server on port %s", port)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
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
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, HEAD")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Max-Age", "3600")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	// Handle preflight OPTIONS request
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Handle HEAD request
	if r.Method == "HEAD" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	// Set content type
	w.Header().Set("Content-Type", "application/json")

	var state common.UserState
	if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
		log.Printf("Error decoding request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Printf("Received prediction request for user: %s", state.Name)

	// Get prediction
	prediction, err := getPrediction(&state)
	if err != nil {
		log.Printf("Error getting prediction: %v", err)
		http.Error(w, fmt.Sprintf("Error getting prediction: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Generated prediction for user: %s", state.Name)

	// Generate images
	var wg sync.WaitGroup
	var imageErrors []error
	images := make([][]byte, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			log.Printf("Generating image %d for user: %s", index+1, state.Name)
			img, err := common.GenerateKandinskyImage(prediction.ImagePrompts[index])
			if err != nil {
				imageErrors = append(imageErrors, fmt.Errorf("error generating image %d: %v", index+1, err))
				return
			}
			images[index] = img
			log.Printf("Generated image %d for user: %s", index+1, state.Name)
		}(i)
	}
	wg.Wait()

	if len(imageErrors) > 0 {
		errMsgs := make([]string, len(imageErrors))
		for i, err := range imageErrors {
			errMsgs[i] = err.Error()
		}
		http.Error(w, fmt.Sprintf("Error generating images: %s", strings.Join(errMsgs, "; ")), http.StatusInternalServerError)
		return
	}

	response := common.PredictionResponse{
		Text:    prediction.Text,
		Images:  images,
		Prompts: prediction.ImagePrompts,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully sent prediction for user: %s", state.Name)
}

func getPrediction(state *common.UserState) (*common.Prediction, error) {
	prompt := fmt.Sprintf("Ты - опытный таролог и экстрасенс. Тебе нужно дать предсказание для человека по имени %s (родился(ась) %s). "+
		"Вопрос: %s (сфера: %s). "+
		"Дай подробное предсказание (минимум 2000 символов). В конце предсказания сгенерируй три отдельных промпта для генерации изображений, "+
		"которые будут иллюстрировать твое предсказание. Каждый промпт должен быть на английском языке и содержать описание изображения в стиле Кандинского.",
		state.Name, state.BirthDate, state.Question, state.Mode)

	client := common.NewOpenAIClient(os.Getenv("OPENROUTER_API_KEY"))
	response, err := client.CreateChatCompletion(prompt)
	if err != nil {
		return nil, fmt.Errorf("error creating chat completion: %v", err)
	}

	// Split response into text and prompts
	parts := strings.Split(response, "\n\n")
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid response format")
	}

	text := strings.Join(parts[:len(parts)-3], "\n\n")
	imagePrompts := parts[len(parts)-3:]

	return &common.Prediction{
		Text:         text,
		ImagePrompts: imagePrompts,
	}, nil
}
