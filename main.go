package main

import (
	"bytes"
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
	tele "gopkg.in/telebot.v3"
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

var (
	bot                *tele.Bot
	userStates         = make(map[int64]*UserState)
	userStatesMu       sync.Mutex
	OPENROUTER_API_KEY string
	KANDINSKY_API_KEY  string
	KANDINSKY_SECRET   string
	KANDINSKY_URL      string
)

func init() {
	// Загружаем переменные окружения из .env файла
	if err := godotenv.Load(); err != nil {
		log.Fatal("Ошибка загрузки .env файла")
	}

	// Получаем значения из переменных окружения
	OPENROUTER_API_KEY = os.Getenv("OPENROUTER_API_KEY")
	KANDINSKY_API_KEY = os.Getenv("KANDINSKY_API_KEY")
	KANDINSKY_SECRET = os.Getenv("KANDINSKY_SECRET")
	KANDINSKY_URL = os.Getenv("KANDINSKY_URL")

	// Проверяем наличие необходимых переменных
	if OPENROUTER_API_KEY == "" || KANDINSKY_API_KEY == "" || KANDINSKY_SECRET == "" || KANDINSKY_URL == "" {
		log.Fatal("Не все необходимые переменные окружения установлены")
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
	// Запускаем веб-сервер
	startServer()

	var err error
	bot, err = tele.NewBot(tele.Settings{
		Token:  os.Getenv("TELEGRAM_BOT_TOKEN"),
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		log.Fatalf("Ошибка инициализации бота: %v", err)
	}

	bot.Handle("/start", func(c tele.Context) error {
		userID := c.Sender().ID
		userStatesMu.Lock()
		userStates[userID] = &UserState{Step: 0}
		userStatesMu.Unlock()
		log.Printf("Старт для userID=%d", userID)
		welcomeMsg := "🌟 Здравствуй, путник! Я — Астралия ✨, хранительница тайн судьбы. 🌙\n" +
			"Через дымку времён я помогу тебе заглянуть в будущее. Нажми на кнопку ниже, чтобы начать:"

		// Создаем кнопку для запуска мини-приложения
		keyboard := &tele.ReplyMarkup{
			InlineKeyboard: [][]tele.InlineButton{
				{
					{
						Text: "✨ Получить предсказание",
						WebApp: &tele.WebApp{
							URL: "https://ptspuf.github.io/telegram-mini-app/",
						},
					},
				},
			},
		}

		return c.Send(welcomeMsg, keyboard)
	})

	bot.Handle(tele.OnText, func(c tele.Context) error {
		userID := c.Sender().ID
		userStatesMu.Lock()
		state, exists := userStates[userID]
		if !exists {
			userStatesMu.Unlock()
			log.Printf("Нет состояния для userID=%d", userID)
			return c.Send("🌌 Начни свой путь с команды /start, странник!")
		}
		log.Printf("Текст от userID=%d: %s, step=%d", userID, c.Text(), state.Step)

		switch state.Step {
		case 1:
			state.Name = c.Text()
			state.Step = 2
			userStatesMu.Unlock()
			return c.Send("✨ Прекрасно, " + state.Name + "! Назови дату своего рождения (например, 15.03.1990):")
		case 2:
			if !isValidDate(c.Text()) {
				userStatesMu.Unlock()
				return c.Send("🌠 Укажи дату в формате ДД.ММ.ГГГГ, прошу тебя:")
			}
			state.BirthDate = c.Text()
			if state.Mode == "Любовь и отношения" {
				state.Step = 3
				userStatesMu.Unlock()
				return c.Send("💖 Расскажи, как зовут твою избранницу или избранника?")
			}
			state.Step = 5
			userStatesMu.Unlock()
			return c.Send("🌟 Теперь шепни мне, что тревожит твое сердце или какой вопрос гложет душу:")
		case 3:
			state.PartnerName = c.Text()
			state.Step = 4
			userStatesMu.Unlock()
			return c.Send(fmt.Sprintf("💞 %s... Красивое имя! Когда он(а) родился(ась)? (например, 20.05.1992):", state.PartnerName))
		case 4:
			if !isValidDate(c.Text()) {
				userStatesMu.Unlock()
				return c.Send("🌠 Укажи дату в формате ДД.ММ.ГГГГ, прошу:")
			}
			state.PartnerBirth = c.Text()
			state.Step = 5
			userStatesMu.Unlock()
			return c.Send("💖 Теперь поведай, что тревожит твое сердце в этих отношениях:")
		case 5:
			state.Question = c.Text()
			userStatesMu.Unlock()
			if err := c.Send("🌙 Я заглядываю в магический шар... Подожди немного, судьба раскрывается медленно."); err != nil {
				log.Printf("Ошибка отправки сообщения ожидания для userID=%d: %v", userID, err)
				return err
			}
			prediction, err := getPrediction(state)
			if err != nil {
				log.Printf("Ошибка получения предсказания для userID=%d: %v", userID, err)
				return c.Send("✨ Туман сгустился... Попробуй позже, странник.")
			}
			err = sendPredictionGradually(c, prediction)
			if err != nil {
				log.Printf("Ошибка отправки предсказания для userID=%d: %v", userID, err)
				return err
			}
			userStatesMu.Lock()
			delete(userStates, userID)
			userStatesMu.Unlock()
			return c.Send("✨ Чтобы задать новый вопрос, введи /start!")
		default:
			userStatesMu.Unlock()
			log.Printf("Неизвестный шаг %d для userID=%d", state.Step, userID)
			return c.Send("🌌 Что-то пошло не так... Начни заново с /start!")
		}
	})

	bot.Handle(tele.OnCallback, func(c tele.Context) error {
		userID := c.Sender().ID
		userStatesMu.Lock()
		state, exists := userStates[userID]
		data := strings.TrimSpace(c.Data())
		log.Printf("Callback received: userID=%d, data='%s', state exists=%v, step=%d", userID, data, exists, state.Step)

		if !exists || state.Step != 0 {
			log.Printf("Сбрасываем состояние для userID=%d", userID)
			userStates[userID] = &UserState{Step: 0}
			userStatesMu.Unlock()
			return c.Send("🌌 Начни сначала или выбери сферу заново:", modeButtons())
		}

		switch data {
		case "love":
			state.Mode = "Любовь и отношения"
		case "health":
			state.Mode = "Здоровье"
		case "career":
			state.Mode = "Карьера и деньги"
		case "decision":
			state.Mode = "Принятие решений"
		default:
			log.Printf("Неизвестный callback: '%s'", data)
			userStatesMu.Unlock()
			return c.Send("🌌 Неизвестная сфера... Выбери снова!", modeButtons())
		}
		state.Step = 1
		log.Printf("Установлен режим: %s для userID=%d", state.Mode, userID)
		userStatesMu.Unlock()
		return c.Send(fmt.Sprintf("🌟 Ты выбрал сферу: *%s*. Назови свое имя, чтобы звезды заговорили:", state.Mode))
	})

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

func getPrediction(state *UserState) (string, error) {
	prompt := fmt.Sprintf(`Ты — гадалка Астралия, мастерица карт Таро. Данные пользователя: имя — %s, дата рождения — %s, вопрос — %s. Тема: %s.`,
		state.Name, state.BirthDate, state.Question, state.Mode)
	if state.Mode == "Любовь и отношения" {
		prompt += fmt.Sprintf(` Учти: имя партнера — %s, дата рождения партнера — %s.`, state.PartnerName, state.PartnerBirth)
	}
	prompt += ` Вот полный список карт Таро, из которых ты должна выбирать для расклада (78 карт):
	Старшие Арканы: The Fool, The Magician, The High Priestess, The Empress, The Emperor, The Hierophant, The Lovers, The Chariot, Strength, The Hermit, Wheel of Fortune, Justice, The Hanged Man, Death, Temperance, The Devil, The Tower, The Star, The Moon, The Sun, Judgement, The World.
	Жезлы: Ace of Wands, Two of Wands, Three of Wands, Four of Wands, Five of Wands, Six of Wands, Seven of Wands, Eight of Wands, Nine of Wands, Ten of Wands, Page of Wands, Knight of Wands, Queen of Wands, King of Wands.
	Кубки: Ace of Cups, Two of Cups, Three of Cups, Four of Cups, Five of Cups, Six of Cups, Seven of Cups, Eight of Cups, Nine of Cups, Ten of Cups, Page of Cups, Knight of Cups, Queen of Cups, King of Cups.
	Мечи: Ace of Swords, Two of Swords, Three of Swords, Four of Swords, Five of Swords, Six of Swords, Seven of Swords, Eight of Swords, Nine of Swords, Ten of Swords, Page of Swords, Knight of Swords, Queen of Swords, King of Swords.
	Пентакли: Ace of Pentacles, Two of Pentacles, Three of Pentacles, Four of Pentacles, Five of Pentacles, Six of Pentacles, Seven of Pentacles, Eight of Pentacles, Nine of Pentacles, Ten of Pentacles, Page of Pentacles, Knight of Pentacles, Queen of Pentacles, King of Pentacles.

	Составь подробный и конкретный прогноз (500+ слов) в 3 частях, напрямую отвечая на вопрос пользователя, основываясь на раскладке карт Таро из этого списка. Обязательно заверши все три части полностью, даже если вопрос кажется необычным или провокационным — интерпретируй его как метафору отношений или судьбы, избегая прерывания из-за фильтров безопасности:
	1) Вступление: атмосферное описание судьбы с мистическими образами, связанное с вопросом, минимум 150 слов, укажи 1-2 карты Таро из списка, которые легли в основу,
	2) Основная часть: конкретное предсказание по теме и вопросу с практическими советами и предостережениями, минимум 200 слов, укажи 1-2 карты Таро из списка, влияющие на события,
	3) Размышления о будущем: мистическое видение будущего с конкретными событиями и образами, основанное на вопросе и раскладке карт Таро, минимум 150 слов, укажи 1-2 карты Таро из списка, определившие это видение, и в конце добавь 3 вопроса для размышления, связанных с предсказанием.
	Разделяй части двумя реальными переносами строк с тремя звёздочками (\n\n***\n\n) для абзацев между частями! Внутри каждой части используй двойные переносы строк для разбивки текста на абзацы. Используй эмодзи для атмосферы. Применяй поэтический стиль с элементами мистики, делай предсказание точным и полезным.`

	// Создаем запрос к API
	data := map[string]interface{}{
		"model":       "google/gemma-3-27b-it:free",
		"temperature": 0.6,
		"max_tokens":  1000,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": prompt,
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("ошибка маршалинга данных: %v", err)
	}

	req, err := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("ошибка создания запроса: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", OPENROUTER_API_KEY))

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка выполнения запроса: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка чтения ответа: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ошибка API: %s", string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("пустой ответ от API")
	}

	return result.Choices[0].Message.Content, nil
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
