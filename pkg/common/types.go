package common

// UserState представляет состояние пользователя
type UserState struct {
	Age    int    `json:"age"`
	Gender string `json:"gender"`
}

// Prediction представляет предсказание
type Prediction struct {
	Text         string   `json:"text"`
	ImagePrompts []string `json:"imagePrompts"`
}

// PredictionResponse представляет ответ с предсказанием
type PredictionResponse struct {
	Text    string   `json:"text"`
	Images  [][]byte `json:"images"`
	Prompts []string `json:"prompts"`
}

// KandinskyGenerateRequest представляет запрос к API Kandinsky
type KandinskyGenerateRequest struct {
	Type           string `json:"type"`
	NumImages      int    `json:"numImages"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	GenerateParams struct {
		Query string `json:"query"`
	} `json:"generateParams"`
}

// KandinskyStatusResponse представляет ответ от API Kandinsky
type KandinskyStatusResponse struct {
	UUID     string   `json:"uuid"`
	Status   string   `json:"status"`
	Images   []string `json:"images"`
	Error    string   `json:"errorDescription"`
	Censored bool     `json:"censored"`
}
