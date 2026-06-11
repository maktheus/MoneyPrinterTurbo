package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const telegramAPI = "https://api.telegram.org/bot"

type TelegramClient struct {
	botToken string
	chatID   string
	http     *http.Client
}

func NewTelegramClient(botToken, chatID string) *TelegramClient {
	return &TelegramClient{
		botToken: botToken,
		chatID:   chatID,
		http:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *TelegramClient) Send(text string) error {
	if t.botToken == "" || t.chatID == "" {
		return nil // silently skip if not configured
	}

	payload := map[string]string{
		"chat_id":    t.chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	body, _ := json.Marshal(payload)

	resp, err := t.http.Post(
		fmt.Sprintf("%s%s/sendMessage", telegramAPI, t.botToken),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("telegram returned %d", resp.StatusCode)
	}
	return nil
}
