package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type TelegramSink struct {
	endpoint string
	chatID   string
	client   *http.Client
}

func NewTelegramSink(baseURL, token, chatID string, client *http.Client) *TelegramSink {
	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", strings.TrimRight(baseURL, "/"), token)
	return &TelegramSink{endpoint: endpoint, chatID: chatID, client: client}
}

func (s *TelegramSink) Name() string {
	return "telegram"
}

func (s *TelegramSink) Send(ctx context.Context, notification Notification) error {
	payload := map[string]string{
		"chat_id": s.chatID,
		"text":    notification.Text(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal telegram payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("post telegram message: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("telegram responded with status %d", resp.StatusCode)
	}
	return nil
}
