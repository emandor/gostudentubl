package llm

import (
"bytes"
"context"
"encoding/json"
"fmt"
"io"
"net/http"
)

// LLMClient is the interface for getting AI suggestions.
type LLMClient interface {
GetSuggestion(ctx context.Context, req SuggestionRequest) (SuggestionResponse, error)
}

type SuggestionRequest struct {
EventType  string
CourseName string
ItemName   string
ItemTitle  string
Content    string
}

type SuggestionResponse struct {
Suggestion string
Model      string
TokensUsed int
}

// Client implements LLMClient via the OpenRouter API.
type Client struct {
Endpoint string
APIKey   string
Model    string
HC       *http.Client
}

type chatRequest struct {
Model    string        `json:"model"`
Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
Role    string `json:"role"`
Content string `json:"content"`
}

type chatResponse struct {
Choices []struct {
Message struct {
Content string `json:"content"`
} `json:"message"`
} `json:"choices"`
Usage struct {
TotalTokens int `json:"total_tokens"`
} `json:"usage"`
Model string `json:"model"`
}

const systemPrompt = `You are an academic assistant helping a university student. 
When given information about a quiz or assignment, provide:
1. A brief summary of what's expected
2. Key topics/concepts the student should review
3. Practical tips for completing the task
4. Time management suggestions based on the deadline

Keep your response concise and actionable. Use Indonesian language mixed with English technical terms where appropriate, as the student is from an Indonesian university.`

func (c *Client) GetSuggestion(ctx context.Context, req SuggestionRequest) (SuggestionResponse, error) {
userContent := fmt.Sprintf(
"Help me with this %s:\n\nCourse: %s\nTopic: %s\nItem: %s\n\nDetails:\n%s",
req.EventType,
req.CourseName,
req.ItemTitle,
req.ItemName,
req.Content,
)

body := chatRequest{
Model: c.Model,
Messages: []chatMessage{
{Role: "system", Content: systemPrompt},
{Role: "user", Content: userContent},
},
}

jsonBody, err := json.Marshal(body)
if err != nil {
return SuggestionResponse{}, fmt.Errorf("marshal request: %w", err)
}

httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(jsonBody))
if err != nil {
return SuggestionResponse{}, fmt.Errorf("create request: %w", err)
}
httpReq.Header.Set("Content-Type", "application/json")
httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

resp, err := c.HC.Do(httpReq)
if err != nil {
return SuggestionResponse{}, fmt.Errorf("do request: %w", err)
}
defer resp.Body.Close()

respBody, err := io.ReadAll(resp.Body)
if err != nil {
return SuggestionResponse{}, fmt.Errorf("read response: %w", err)
}

if resp.StatusCode != http.StatusOK {
return SuggestionResponse{}, fmt.Errorf("openrouter api error (status %d): %s", resp.StatusCode, string(respBody))
}

var chatResp chatResponse
if err := json.Unmarshal(respBody, &chatResp); err != nil {
return SuggestionResponse{}, fmt.Errorf("unmarshal response: %w", err)
}

if len(chatResp.Choices) == 0 {
return SuggestionResponse{}, fmt.Errorf("no choices in response")
}

return SuggestionResponse{
Suggestion: chatResp.Choices[0].Message.Content,
Model:      chatResp.Model,
TokensUsed: chatResp.Usage.TotalTokens,
}, nil
}
