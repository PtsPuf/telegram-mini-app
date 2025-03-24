package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type OpenAIClient struct {
	apiKey     string
	httpClient *http.Client
}

type OpenAIRequest struct {
	Model       string          `json:"model"`
	Messages    []OpenAIMessage `json:"messages"`
	Temperature float64         `json:"temperature"`
	MaxTokens   int             `json:"max_tokens"`
}

type OpenAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenAIResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int    `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

func NewOpenAIClient(apiKey string) *OpenAIClient {
	return &OpenAIClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (c *OpenAIClient) CreateChatCompletion(prompt string) (string, error) {
	log.Printf("Starting chat completion with prompt length: %d", len(prompt))

	requestURL := "https://openrouter.ai/api/v1/chat/completions"
	requestBody := OpenAIRequest{
		Model: "anthropic/claude-3-haiku",
		Messages: []OpenAIMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Temperature: 0.7,
		MaxTokens:   4000,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %v", err)
	}

	log.Printf("Sending request to OpenRouter API, data size: %d bytes", len(jsonData))
	req, err := http.NewRequest("POST", requestURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("HTTP-Referer", "https://telegram-mini-app.onrender.com")
	req.Header.Set("X-Title", "Telegram Mini App")

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	log.Printf("Received response from OpenRouter API in %s, status code: %d", time.Since(start), resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("Error response from OpenRouter API: %s", string(body))
		return "", fmt.Errorf("API request failed with status code %d: %s", resp.StatusCode, string(body))
	}

	var openAIResponse OpenAIResponse
	if err := json.Unmarshal(body, &openAIResponse); err != nil {
		log.Printf("Failed to unmarshal response: %v, body: %s", err, string(body))
		return "", fmt.Errorf("failed to unmarshal response: %v", err)
	}

	if len(openAIResponse.Choices) == 0 {
		log.Printf("Empty choices in response: %s", string(body))
		return "", fmt.Errorf("no choices in response")
	}

	content := openAIResponse.Choices[0].Message.Content
	log.Printf("Successfully received chat completion, content length: %d", len(content))

	return content, nil
}
