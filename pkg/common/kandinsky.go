package common

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
	"time"
)

// GenerateKandinskyImage генерирует изображение с помощью Kandinsky API
func GenerateKandinskyImage(prompt string) ([]byte, error) {
	log.Printf("Начало генерации изображения: %s", prompt)

	uuid, err := createGenerationTask(prompt)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания задачи: %v", err)
	}

	log.Printf("Задача создана, UUID: %s", uuid)

	// Ждем завершения генерации
	var imageData []byte
	for i := 0; i < 30; i++ { // Максимум 30 попыток (5 минут)
		status, err := checkGenerationStatus(uuid)
		if err != nil {
			return nil, fmt.Errorf("ошибка проверки статуса: %v", err)
		}

		if status.Status == "DONE" {
			if len(status.Images) == 0 {
				return nil, fmt.Errorf("изображение не сгенерировано")
			}

			// Декодируем base64 в байты
			imageData, err = base64.StdEncoding.DecodeString(status.Images[0])
			if err != nil {
				return nil, fmt.Errorf("ошибка декодирования изображения: %v", err)
			}
			break
		} else if status.Status == "FAILED" {
			return nil, fmt.Errorf("генерация не удалась: %s", status.Error)
		}

		time.Sleep(10 * time.Second)
	}

	if imageData == nil {
		return nil, fmt.Errorf("превышено время ожидания генерации")
	}

	return imageData, nil
}

func createGenerationTask(prompt string) (string, error) {
	apiKey := os.Getenv("KANDINSKY_API_KEY")
	secret := os.Getenv("KANDINSKY_SECRET")
	apiURL := os.Getenv("KANDINSKY_URL")

	if apiKey == "" || secret == "" || apiURL == "" {
		return "", fmt.Errorf("не установлены переменные окружения для Kandinsky API")
	}

	// Создаем запрос
	reqBody := KandinskyGenerateRequest{
		Type:      "GENERATE",
		NumImages: 1,
		Width:     1024,
		Height:    1024,
	}
	reqBody.GenerateParams.Query = prompt

	// Создаем multipart форму
	var b bytes.Buffer
	writer := multipart.NewWriter(&b)

	// Добавляем API ключ
	err := writer.WriteField("key", apiKey)
	if err != nil {
		return "", fmt.Errorf("ошибка записи API ключа: %v", err)
	}

	// Добавляем секрет
	err = writer.WriteField("secret", secret)
	if err != nil {
		return "", fmt.Errorf("ошибка записи секрета: %v", err)
	}

	// Добавляем параметры запроса как JSON
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="params"`)
	h.Set("Content-Type", "application/json")
	part, err := writer.CreatePart(h)
	if err != nil {
		return "", fmt.Errorf("ошибка создания части формы: %v", err)
	}

	err = json.NewEncoder(part).Encode(reqBody)
	if err != nil {
		return "", fmt.Errorf("ошибка кодирования параметров: %v", err)
	}

	err = writer.Close()
	if err != nil {
		return "", fmt.Errorf("ошибка закрытия формы: %v", err)
	}

	// Отправляем запрос
	req, err := http.NewRequest("POST", apiURL+"/key/api/v1/text2image/run", &b)
	if err != nil {
		return "", fmt.Errorf("ошибка создания запроса: %v", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка отправки запроса: %v", err)
	}
	defer resp.Body.Close()

	var result KandinskyStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("ошибка декодирования ответа: %v", err)
	}

	if result.Error != "" {
		return "", fmt.Errorf("ошибка API: %s", result.Error)
	}

	return result.UUID, nil
}

func checkGenerationStatus(uuid string) (*KandinskyStatusResponse, error) {
	apiKey := os.Getenv("KANDINSKY_API_KEY")
	secret := os.Getenv("KANDINSKY_SECRET")
	apiURL := os.Getenv("KANDINSKY_URL")

	// Создаем multipart форму
	var b bytes.Buffer
	writer := multipart.NewWriter(&b)

	// Добавляем API ключ
	err := writer.WriteField("key", apiKey)
	if err != nil {
		return nil, fmt.Errorf("ошибка записи API ключа: %v", err)
	}

	// Добавляем секрет
	err = writer.WriteField("secret", secret)
	if err != nil {
		return nil, fmt.Errorf("ошибка записи секрета: %v", err)
	}

	// Добавляем UUID
	err = writer.WriteField("uuid", uuid)
	if err != nil {
		return nil, fmt.Errorf("ошибка записи UUID: %v", err)
	}

	err = writer.Close()
	if err != nil {
		return nil, fmt.Errorf("ошибка закрытия формы: %v", err)
	}

	// Отправляем запрос
	req, err := http.NewRequest("POST", apiURL+"/key/api/v1/text2image/status", &b)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса: %v", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ошибка отправки запроса: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %v", err)
	}

	var result KandinskyStatusResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("ошибка декодирования ответа: %v, тело: %s", err, string(body))
	}

	return &result, nil
}
