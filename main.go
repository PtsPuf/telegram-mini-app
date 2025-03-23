package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/sashabaranov/go-openai"
	tele "gopkg.in/telebot.v3"
)

var (
	bot                *tele.Bot
	userStates         = make(map[int64]*UserState)
	userStatesMu       sync.RWMutex
	OPENROUTER_API_KEY string
	KANDINSKY_API_KEY  string
	KANDINSKY_SECRET   string
	KANDINSKY_URL      string
)

type UserState struct {
	Name         string
	BirthDate    string
	Question     string
	Mode         string
	PartnerName  string
	PartnerBirth string
	Step         int
}

func init() {
	// Получаем значения из переменных окружения
	OPENROUTER_API_KEY = os.Getenv("OPENROUTER_API_KEY")
	KANDINSKY_API_KEY = os.Getenv("KANDINSKY_API_KEY")
	KANDINSKY_SECRET = os.Getenv("KANDINSKY_SECRET")
	KANDINSKY_URL = os.Getenv("KANDINSKY_URL")

	// Проверяем только критически важные переменные
	if OPENROUTER_API_KEY == "" {
		log.Printf("ВНИМАНИЕ: OPENROUTER_API_KEY не установлен")
	}
}

type KandinskyGenerateRequest struct {
	Type           string `json:"type"`
	NumImages      int    `json:"numImages"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	GenerateParams struct {
		Query string `json:"query"`
	} `json:"generateParams"`
}

type KandinskyStatusResponse struct {
	UUID     string   `json:"uuid"`
	Status   string   `json:"status"`
	Images   []string `json:"images"`
	Error    string   `json:"errorDescription"`
	Censored bool     `json:"censored"`
}

func main() {
	// Проверяем наличие переменных окружения
	if os.Getenv("TELEGRAM_BOT_TOKEN") == "" {
		// Если переменных нет, пробуем загрузить .env файл
		if err := godotenv.Load(); err != nil {
			log.Printf("Ошибка загрузки .env файла: %v", err)
		}
	}

	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN не установлен")
	}

	// Проверяем наличие необходимых API ключей
	if OPENROUTER_API_KEY == "" {
		log.Fatal("OPENROUTER_API_KEY не установлен")
	}

	pref := tele.Settings{
		Token: botToken,
	}

	bot, err := tele.NewBot(pref)
	if err != nil {
		log.Fatal(err)
		return
	}

	// Получаем порт из переменной окружения или используем значение по умолчанию
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Запускаем веб-сервер
	go startServer(port)

	// Запускаем бота
	log.Println("Бот запущен...")
	bot.Start()
}

func modeButtons() *tele.ReplyMarkup {
	menu := &tele.ReplyMarkup{ResizeKeyboard: true}
	btnLove := menu.Data("💖 Любовь и отношения", "love")
	btnHealth := menu.Data("🌿 Здоровье", "health")
	btnCareer := menu.Data("💰 Карьера и деньги", "career")
	btnDecision := menu.Data("🌀 Принятие решений", "decision")
	menu.Inline(
		menu.Row(btnLove),
		menu.Row(btnHealth),
		menu.Row(btnCareer),
		menu.Row(btnDecision),
	)
	return menu
}

func isValidDate(date string) bool {
	_, err := time.Parse("02.01.2006", date)
	return err == nil
}

func getPrediction(question string, cards []string) (string, error) {
	config := openai.DefaultConfig(OPENROUTER_API_KEY)
	config.BaseURL = "https://openrouter.ai/api/v1"
	client := openai.NewClientWithConfig(config)

	// Формируем промпт с учетом вопроса и выбранных карт
	prompt := fmt.Sprintf(`Ты - опытная гадалка на картах Таро. Пользователь задал вопрос: "%s"
Выпавшие карты: %s

Сделай подробное предсказание, которое:
1. Напрямую отвечает на вопрос пользователя
2. Учитывает значение каждой выбранной карты
3. Объясняет, как карты связаны с вопросом
4. Дает конкретные рекомендации

Формат ответа:
1. Краткое вступление, связывающее вопрос с выбранными картами
2. Подробное толкование каждой карты в контексте вопроса
3. Общее предсказание, объединяющее значения всех карт
4. Конкретные рекомендации для пользователя

Пиши живым, эмоциональным языком, но сохраняй профессионализм.`, question, strings.Join(cards, ", "))

	resp, err := client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: "openai/gpt-4",
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
		return "", fmt.Errorf("ошибка при получении предсказания: %v", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("не получен ответ от модели")
	}

	return resp.Choices[0].Message.Content, nil
}

func generateKandinskyImage(prompt string) ([]byte, error) {
	log.Printf("Начало генерации изображения: %s", prompt)

	uuid, err := createGenerationTask(prompt)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания задачи: %v", err)
	}

	images, err := checkGenerationStatus(uuid)
	if err != nil {
		return nil, fmt.Errorf("ошибка проверки статуса: %v", err)
	}

	decoded, err := base64.StdEncoding.DecodeString(images[0])
	if err != nil {
		return nil, fmt.Errorf("ошибка декодирования base64: %v", err)
	}

	log.Printf("Изображение успешно сгенерировано, размер: %d байт", len(decoded))
	return decoded, nil
}

func createGenerationTask(prompt string) (string, error) {
	params := KandinskyGenerateRequest{
		Type:      "GENERATE",
		NumImages: 1,
		Width:     1024,
		Height:    1024,
		GenerateParams: struct {
			Query string `json:"query"`
		}{
			Query: prompt,
		},
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return "", fmt.Errorf("ошибка сериализации параметров: %v", err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	paramsPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": []string{`form-data; name="params"`},
		"Content-Type":        []string{"application/json"},
	})
	if err != nil {
		return "", fmt.Errorf("ошибка создания поля params: %v", err)
	}
	if _, err := paramsPart.Write(paramsJSON); err != nil {
		return "", fmt.Errorf("ошибка записи params: %v", err)
	}

	if err := writer.WriteField("model_id", "4"); err != nil {
		return "", fmt.Errorf("ошибка добавления model_id: %v", err)
	}

	writer.Close()

	req, err := http.NewRequest("POST", KANDINSKY_URL+"key/api/v1/text2image/run", body)
	if err != nil {
		return "", fmt.Errorf("ошибка создания запроса: %v", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Key", fmt.Sprintf("Key %s", KANDINSKY_API_KEY))
	req.Header.Set("X-Secret", fmt.Sprintf("Secret %s", KANDINSKY_SECRET))

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка отправки запроса: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка чтения ответа: %v", err)
	}

	log.Printf("Ответ от Kandinsky API: %s", respBody)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, respBody)
	}

	var result struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("ошибка парсинга JSON: %v", err)
	}

	return result.UUID, nil
}

func checkGenerationStatus(uuid string) ([]string, error) {
	backoff := 2 * time.Second
	maxAttempts := 10

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		time.Sleep(backoff)
		backoff = time.Duration(float64(backoff) * 1.5)

		req, err := http.NewRequest("GET", KANDINSKY_URL+"key/api/v1/text2image/status/"+uuid, nil)
		if err != nil {
			return nil, fmt.Errorf("ошибка создания запроса: %v", err)
		}

		req.Header.Set("X-Key", fmt.Sprintf("Key %s", KANDINSKY_API_KEY))
		req.Header.Set("X-Secret", fmt.Sprintf("Secret %s", KANDINSKY_SECRET))

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Ошибка запроса статуса (попытка %d): %v", attempt, err)
			continue
		}

		var statusResp KandinskyStatusResponse
		if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("ошибка декодирования статуса: %v", err)
		}
		resp.Body.Close()

		log.Printf("Статус генерации (попытка %d): %s", attempt, statusResp.Status)

		switch statusResp.Status {
		case "DONE":
			return statusResp.Images, nil
		case "FAILED":
			return nil, fmt.Errorf("ошибка API: %s", statusResp.Error)
		case "CENSORED":
			return nil, fmt.Errorf("цензура: %t", statusResp.Censored)
		}
	}
	return nil, fmt.Errorf("максимальное количество попыток (%d) достигнуто", maxAttempts)
}

func sendPredictionGradually(c tele.Context, prediction string) error {
	prediction = strings.ReplaceAll(prediction, `\n\n`, "\n\n")
	prediction = strings.ReplaceAll(prediction, `\n`, "\n")

	// Сначала пробуем разделить по явному разделителю \n\n***\n\n
	parts := strings.Split(prediction, "\n\n***\n\n")

	// Фильтруем пустые части
	filteredParts := make([]string, 0, 3)
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			filteredParts = append(filteredParts, trimmed)
		}
	}

	// Если разделение по \n\n***\n\n не дало трёх частей, делим текст вручную
	if len(filteredParts) != 3 {
		log.Printf("Некорректное количество частей после разделения по *** (%d): %v", len(filteredParts), filteredParts)
		// Разбиваем текст на абзацы по \n\n
		paragraphs := strings.Split(prediction, "\n\n")
		filteredParts = make([]string, 0, 3)
		for _, para := range paragraphs {
			trimmed := strings.TrimSpace(para)
			if trimmed != "" {
				filteredParts = append(filteredParts, trimmed)
			}
		}

		// Если абзацев меньше 3 или больше, группируем их в три части
		if len(filteredParts) != 3 {
			log.Printf("Разделение по абзацам дало %d частей: %v", len(filteredParts), filteredParts)
			adjustedParts := make([]string, 3)
			totalParagraphs := len(filteredParts)
			if totalParagraphs == 0 {
				adjustedParts[0] = "Туман скрывает начало твоего пути, но звезды шепчут о переменах..."
				adjustedParts[1] = "Твой путь пока неясен, но надежда сияет впереди..."
				adjustedParts[2] = "Будущее таит загадки, но завершение цикла близко..."
			} else {
				// Делим абзацы примерно поровну
				third := totalParagraphs / 3
				if third == 0 {
					third = 1
				}
				start := 0
				for i := 0; i < 3 && start < totalParagraphs; i++ {
					end := start + third
					if i == 2 || end > totalParagraphs {
						end = totalParagraphs
					}
					adjustedParts[i] = strings.Join(filteredParts[start:end], "\n\n")
					start = end
				}
				// Если остались пустые части, заполняем их
				for i := range adjustedParts {
					if adjustedParts[i] == "" {
						switch i {
						case 0:
							adjustedParts[i] = "Туман скрывает начало твоего пути, но звезды шепчут о переменах..."
						case 1:
							adjustedParts[i] = "Твой путь пока неясен, но надежда сияет впереди..."
						case 2:
							adjustedParts[i] = "Будущее таит загадки, но завершение цикла близко..."
						}
					}
				}
			}
			filteredParts = adjustedParts
		}
	}

	log.Printf("Окончательные части предсказания: %v", filteredParts)

	// Отправка вступления
	log.Printf("Отправка вступления")
	if err := c.Send(fmt.Sprintf("🌙 *Вступление судьбы* 🌙\n\n%s", filteredParts[0])); err != nil {
		log.Printf("Ошибка отправки вступления: %v", err)
		return err
	}
	time.Sleep(1 * time.Second)
	imgPrompt1 := generateImagePrompt(filteredParts[0])
	log.Printf("Генерация первого изображения с промптом: %s", imgPrompt1)
	imgData, err := generateKandinskyImage(imgPrompt1)
	if err == nil {
		log.Printf("Отправка первого изображения")
		imgReader := bytes.NewReader(imgData)
		if err := c.Send(&tele.Photo{File: tele.FromReader(imgReader), Caption: "✨ Карты раскрывают свои тайны..."}); err != nil {
			log.Printf("Ошибка отправки первого изображения: %v", err)
		}
	} else {
		log.Printf("Ошибка генерации первого изображения: %v", err)
		if err := c.Send("✨ Карты скрыты туманом, но предсказание продолжается..."); err != nil {
			log.Printf("Ошибка отправки сообщения о сбое первого изображения: %v", err)
		}
	}

	// Отправка основной части
	time.Sleep(1 * time.Second)
	log.Printf("Отправка основной части")
	if err := c.Send(fmt.Sprintf("🌟 *Путь твоей судьбы* 🌟\n\n%s", filteredParts[1])); err != nil {
		log.Printf("Ошибка отправки основной части: %v", err)
		return err
	}
	time.Sleep(1 * time.Second)
	imgPrompt2 := generateImagePrompt(filteredParts[1])
	log.Printf("Генерация второго изображения с промптом: %s", imgPrompt2)
	imgData, err = generateKandinskyImage(imgPrompt2)
	if err == nil {
		log.Printf("Отправка второго изображения")
		imgReader := bytes.NewReader(imgData)
		if err := c.Send(&tele.Photo{File: tele.FromReader(imgReader), Caption: "🌌 Таро шепчет о грядущем..."}); err != nil {
			log.Printf("Ошибка отправки второго изображения: %v", err)
		}
	} else {
		log.Printf("Ошибка генерации второго изображения: %v", err)
		if err := c.Send("🌌 Карты молчат, но звезды говорят..."); err != nil {
			log.Printf("Ошибка отправки сообщения о сбое второго изображения: %v", err)
		}
	}

	// Отправка размышлений
	time.Sleep(1 * time.Second)
	log.Printf("Отправка заключения")
	if err := c.Send(fmt.Sprintf("🌀 *Размышления о будущем* 🌀\n\n%s", filteredParts[2])); err != nil {
		log.Printf("Ошибка отправки заключения: %v", err)
		return err
	}
	log.Printf("Предсказание полностью отправлено")
	return nil
}

func generateImagePrompt(text string) string {
	taroCards := map[string]string{
		"The Fool":            "A whimsical fool dancing under a bright sky with mystical symbols",
		"The Magician":        "A powerful magician casting spells with glowing tarot cards",
		"The High Priestess":  "A mysterious priestess in a moonlit temple surrounded by secrets",
		"The Empress":         "A radiant empress in a lush garden with golden light",
		"The Emperor":         "A stern emperor on a throne with fiery skies",
		"The Hierophant":      "A wise priest in a sacred hall with glowing scrolls",
		"The Lovers":          "Two lovers under a starry sky with mystical intertwining fates",
		"The Chariot":         "A warrior in a chariot charging through a cosmic battlefield",
		"Strength":            "A gentle figure taming a lion under a golden sun",
		"The Hermit":          "A lone hermit with a lantern in a foggy night",
		"Wheel of Fortune":    "A spinning wheel of fate under a cosmic sky",
		"Justice":             "A figure with scales and a sword in a hall of light",
		"The Hanged Man":      "A figure hanging upside down in a mystical glow",
		"Death":               "A skeletal figure in a dark misty landscape with renewal symbols",
		"Temperance":          "An angelic figure blending water under a rainbow",
		"The Devil":           "A chained figure under a fiery pentagram",
		"The Tower":           "A crumbling tower struck by lightning in a stormy night",
		"The Star":            "A serene figure under a starry sky pouring water into a glowing pool",
		"The Moon":            "A moonlit scene with howling wolves and mysterious shadows",
		"The Sun":             "A bright sun illuminating a joyful child in a golden field",
		"Judgement":           "Figures rising from graves under a trumpet's call",
		"The World":           "A dancer in a cosmic circle with glowing symbols",
		"Ace of Wands":        "A glowing wand igniting flames in a mystical void",
		"Two of Wands":        "A figure holding two wands overlooking a fiery horizon",
		"Three of Wands":      "A figure watching ships sail under a blazing sky",
		"Four of Wands":       "A celebration with four wands under a golden arch",
		"Five of Wands":       "Five figures clashing with wands in a stormy scene",
		"Six of Wands":        "A victorious rider with a wand crowned in laurels",
		"Seven of Wands":      "A figure defending with a wand against shadows",
		"Eight of Wands":      "Eight wands flying through a fiery sky",
		"Nine of Wands":       "A weary figure guarding with nine wands",
		"Ten of Wands":        "A burdened figure carrying ten wands in a dim light",
		"Page of Wands":       "A young explorer with a wand in a blazing field",
		"Knight of Wands":     "A knight charging with a wand through flames",
		"Queen of Wands":      "A queen with a wand seated in a fiery throne",
		"King of Wands":       "A king with a wand ruling over a burning landscape",
		"Ace of Cups":         "A glowing chalice overflowing with mystical water",
		"Two of Cups":         "Two figures sharing cups under a gentle sky",
		"Three of Cups":       "Three figures dancing with cups in a joyful scene",
		"Four of Cups":        "A figure ignoring four cups under a cloudy sky",
		"Five of Cups":        "A figure mourning over spilled cups in a dark mist",
		"Six of Cups":         "Children exchanging cups in a nostalgic glow",
		"Seven of Cups":       "A figure dreaming of seven cups in a misty vision",
		"Eight of Cups":       "A figure walking away from eight cups under a moon",
		"Nine of Cups":        "A satisfied figure with nine cups in a golden light",
		"Ten of Cups":         "A family under a rainbow with ten cups",
		"Page of Cups":        "A young dreamer with a cup by a serene river",
		"Knight of Cups":      "A knight offering a cup on a misty shore",
		"Queen of Cups":       "A queen with a cup seated by a tranquil sea",
		"King of Cups":        "A king with a cup ruling over calm waters",
		"Ace of Swords":       "A glowing sword piercing a stormy sky",
		"Two of Swords":       "A blindfolded figure with two crossed swords",
		"Three of Swords":     "A heart pierced by three swords in a rainy scene",
		"Four of Swords":      "A resting figure with four swords in a quiet tomb",
		"Five of Swords":      "A victor with five swords in a tense battlefield",
		"Six of Swords":       "A boat with six swords crossing a misty river",
		"Seven of Swords":     "A figure sneaking away with seven swords",
		"Eight of Swords":     "A bound figure surrounded by eight swords",
		"Nine of Swords":      "A figure in despair with nine swords overhead",
		"Ten of Swords":       "A fallen figure pierced by ten swords under a dark sky",
		"Page of Swords":      "A young spy with a sword in a windy field",
		"Knight of Swords":    "A knight charging with a sword through a storm",
		"Queen of Swords":     "A queen with a sword seated in a clear sky",
		"King of Swords":      "A king with a sword ruling from a stormy throne",
		"Ace of Pentacles":    "A glowing pentacle in a fertile garden",
		"Two of Pentacles":    "A juggler balancing two pentacles by the sea",
		"Three of Pentacles":  "A craftsman working with three pentacles",
		"Four of Pentacles":   "A miser clutching four pentacles in a dim vault",
		"Five of Pentacles":   "Two beggars under five pentacles in a snowy night",
		"Six of Pentacles":    "A generous figure giving six pentacles",
		"Seven of Pentacles":  "A farmer tending seven pentacles in a field",
		"Eight of Pentacles":  "A worker crafting eight pentacles",
		"Nine of Pentacles":   "A lady with nine pentacles in a lush vineyard",
		"Ten of Pentacles":    "A family with ten pentacles in a golden estate",
		"Page of Pentacles":   "A young scholar with a pentacle in a green field",
		"Knight of Pentacles": "A knight with a pentacle riding through farmland",
		"Queen of Pentacles":  "A queen with a pentacle seated in a garden",
		"King of Pentacles":   "A king with a pentacle ruling over a golden land",
	}

	for card, description := range taroCards {
		if strings.Contains(strings.ToLower(text), strings.ToLower(card)) {
			return description + ", mystical tarot style, glowing ethereal atmosphere"
		}
	}

	switch {
	case strings.Contains(strings.ToLower(text), "огонь") || strings.Contains(strings.ToLower(text), "fire"):
		return "A fiery tarot card glowing with mystical flames in a dark void"
	case strings.Contains(strings.ToLower(text), "море") || strings.Contains(strings.ToLower(text), "sea"):
		return "A tarot card with a turbulent sea under a stormy sky, mystical glow"
	case strings.Contains(strings.ToLower(text), "звезды") || strings.Contains(strings.ToLower(text), "stars"):
		return "A tarot card with a starry night sky and glowing celestial symbols"
	case strings.Contains(strings.ToLower(text), "любовь") || strings.Contains(strings.ToLower(text), "love"):
		return "A tarot card with two entwined figures under a glowing heart, mystical aura"
	default:
		return "A mystical tarot card with swirling fates and ethereal light in a dark room"
	}
}
