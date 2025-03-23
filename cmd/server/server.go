package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/PtsPuf/telegram-mini-app/pkg/common"
)

var (
	states = make(map[string]*common.UserState)
	mu     sync.RWMutex
)

func startServer(port string) {
	http.HandleFunc("/prediction", handlePrediction)

	log.Printf("Starting server on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func handlePrediction(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, HEAD")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")

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

	var state common.UserState
	if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
		log.Printf("Error decoding request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get prediction
	prediction, err := getPrediction(&state)
	if err != nil {
		log.Printf("Error getting prediction: %v", err)
		http.Error(w, fmt.Sprintf("Error getting prediction: %v", err), http.StatusInternalServerError)
		return
	}

	// Generate images
	var wg sync.WaitGroup
	var imageErrors []error
	images := make([][]byte, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			img, err := common.GenerateKandinskyImage(prediction.ImagePrompts[index])
			if err != nil {
				imageErrors = append(imageErrors, fmt.Errorf("error generating image %d: %v", index+1, err))
				return
			}
			images[index] = img
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
		return
	}
}

func getPrediction(state *common.UserState) (*common.Prediction, error) {
	prompt := fmt.Sprintf("Ты - опытный таролог и экстрасенс. Тебе нужно дать предсказание для человека, у которого: возраст %d, пол %s. "+
		"Дай общее предсказание на будущее (минимум 2000 символов). В конце предсказания сгенерируй три отдельных промпта для генерации изображений, "+
		"которые будут иллюстрировать твое предсказание. Каждый промпт должен быть на английском языке и содержать описание изображения в стиле Кандинского.",
		state.Age, state.Gender)

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
