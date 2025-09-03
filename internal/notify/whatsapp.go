package notify

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
)

type GroupMessage struct {
	Message string
	GroupID string
}

type WhatsAppPayload struct {
	Message string `json:"message"`
	GroupID string `json:"groupId"`
}

func SendWhatsAppConcurrent(msgs []GroupMessage) {
	var wg sync.WaitGroup
	client := &http.Client{}

	for _, m := range msgs {
		wg.Add(1)
		go func(m GroupMessage) {
			defer wg.Done()

			payload := WhatsAppPayload{
				Message: m.Message,
				GroupID: m.GroupID,
			}

			data, _ := json.Marshal(payload)
			req, _ := http.NewRequest("POST", os.Getenv("WA_ENDPOINT"), bytes.NewBuffer(data))
			req.Header.Set("Authorization", os.Getenv("WA_TOKEN"))
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				log.Printf("[WA] failed send to %s: %v", m.GroupID, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode >= 300 {
				log.Printf("[WA] notify failed to %s: %s", m.GroupID, resp.Status)
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
