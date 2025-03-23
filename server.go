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

func handleWebApp(w http.ResponseWriter, r *http.Request) {
	enableCors(&w)

	if r.Method == "OPTIONS" {
		handleOptions(w, r)
		return
	}

	if r.Method == "GET" {
		http.ServeFile(w, r, "index.html")
		return
	}

	if r.Method == "POST" {
		log.Printf("Получен POST запрос от %s", r.RemoteAddr)

		var state WebAppState
		if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
			log.Printf("Ошибка декодирования JSON: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("Получены данные: %+v", state)

		// Сохраняем состояние
		webAppMu.Lock()
		webAppStates[r.RemoteAddr] = &state
		webAppMu.Unlock()

		// Если это последний шаг, генерируем предсказание
		if state.Step == 5 {
			log.Printf("Начинаем генерацию предсказания для %s", state.Name)
			prediction, err := getPrediction(&UserState{
				Name:         state.Name,
				BirthDate:    state.BirthDate,
				Question:     state.Question,
				Mode:         state.Mode,
				PartnerName:  state.PartnerName,
				PartnerBirth: state.PartnerBirth,
				Step:         state.Step,
			})
			if err != nil {
				log.Printf("Ошибка получения предсказания: %v", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			log.Printf("Предсказание успешно сгенерировано для %s", state.Name)

			// Отправляем предсказание обратно
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]string{
				"prediction": prediction,
			}); err != nil {
				log.Printf("Ошибка отправки ответа: %v", err)
				http.Error(w, "Ошибка отправки ответа", http.StatusInternalServerError)
			}
			return
		}

		// Для остальных шагов просто подтверждаем получение
		w.WriteHeader(http.StatusOK)
	}
}

func startServer(port string) {
	// Настраиваем маршруты
	http.HandleFunc("/", handleWebApp)
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
