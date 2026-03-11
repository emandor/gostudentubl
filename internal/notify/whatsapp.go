package notify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/emandor/gostudentubl/internal/config"
)

type GroupMessage struct {
	Message string
	GroupID string
}

type WhatsAppPayload struct {
	Message string `json:"message"`
	GroupID string `json:"groupId"`
}

func SendWhatsAppReliable(msgs []GroupMessage) error {
	if len(msgs) == 0 {
		return errors.New("no whatsapp messages to send")
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config load: %w", err)
	}
	client := &http.Client{}
	validTargets := 0
	var errs []string
	for _, m := range msgs {
		if strings.TrimSpace(m.GroupID) == "" {
			continue
		}
		validTargets++
		if err := sendWhatsApp(client, cfg.WAEndpoint, cfg.WAToken, m); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", m.GroupID, err))
		}
	}
	if validTargets == 0 {
		return errors.New("no whatsapp group targets configured")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func SendWhatsAppConcurrent(msgs []GroupMessage) {
	var wg sync.WaitGroup
	client := &http.Client{}
	cfg, err := config.Load()

	if err != nil {
		log.Printf("[WA] config load error: %v", err)
		return
	}

	for _, m := range msgs {
		if strings.TrimSpace(m.GroupID) == "" {
			continue
		}
		wg.Add(1)
		go func(m GroupMessage) {
			defer wg.Done()
			if err := sendWhatsApp(client, cfg.WAEndpoint, cfg.WAToken, m); err != nil {
				log.Printf("[WA] notify failed to %s: %v", m.GroupID, err)
			} else {
				log.Printf("[WA] notify sent to %s", m.GroupID)
			}
		}(m)
	}

	// we don't wait for `wg.Wait()` to allow fire-and-forget
	// but if you want a "graceful shutdown", you can choose to wait for all to finish
	go func() {
		wg.Wait()
		log.Println("[WA] all notify tasks completed")
	}()
}

func sendWhatsApp(client *http.Client, waEndpoint, waToken string, m GroupMessage) error {
	payload := WhatsAppPayload{
		Message: m.Message,
		GroupID: m.GroupID,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, waEndpoint, bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", waToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if len(body) > 0 {
			return fmt.Errorf("status %s body=%q", resp.Status, string(body))
		}
		return fmt.Errorf("status %s", resp.Status)
	}

	return nil
}
