package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
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
	if r.Method == "GET" {
		http.ServeFile(w, r, "index.html")
		return
	}

	if r.Method == "POST" {
		var state WebAppState
		if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Сохраняем состояние
		webAppMu.Lock()
		webAppStates[r.RemoteAddr] = &state
		webAppMu.Unlock()

		// Если это последний шаг, генерируем предсказание
		if state.Step == 5 {
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
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// Отправляем предсказание обратно
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"prediction": prediction,
			})
			return
		}

		// Для остальных шагов просто подтверждаем получение
		w.WriteHeader(http.StatusOK)
	}
}

func startServer() {
	// Настраиваем маршруты
	http.HandleFunc("/", handleWebApp)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Запускаем сервер
	log.Println("Сервер запущен на http://localhost:8080")
	go http.ListenAndServe(":8080", nil)
}
