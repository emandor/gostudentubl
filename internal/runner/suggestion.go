package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/emandor/gostudentubl/internal/notify"
)

func (r *Runner) processSuggestions(ctx context.Context) error {
	items, err := r.NotificationStore.ListNeedingSuggestion(ctx, 5)
	if err != nil {
		return fmt.Errorf("list needing suggestion: %w", err)
	}
	if len(items) == 0 {
		return nil
	}

	r.Log.Info().Int("items_needing_suggestion", len(items)).Msg("processing AI suggestions")

	for _, item := range items {
		// Mark as pending to prevent re-processing
		if err := r.NotificationStore.UpdateSuggestion(ctx, item.ID, "pending", ""); err != nil {
			r.Log.Warn().Err(err).Str("item", item.ItemName).Msg("mark suggestion pending failed")
			continue
		}

		content := item.LastError // raw content was loaded into LastError by the store query
		if content == "" {
			content = fmt.Sprintf("Course: %s\nTopic: %s\nItem: %s\nType: %s",
				item.CourseName, item.ItemTitle, item.ItemName, item.EventType)
		}

		resp, err := r.LLMClient.GetSuggestion(ctx, SuggestionRequest{
			EventType:  item.EventType,
			CourseName: item.CourseName,
			ItemName:   item.ItemName,
			ItemTitle:  item.ItemTitle,
			Content:    content,
		})
		if err != nil {
			r.Log.Warn().Err(err).Str("item", item.ItemName).Msg("LLM suggestion request failed")
			if updateErr := r.NotificationStore.UpdateSuggestion(ctx, item.ID, "error", err.Error()); updateErr != nil {
				r.Log.Warn().Err(updateErr).Msg("update suggestion error status failed")
			}
			continue
		}

		if err := r.NotificationStore.UpdateSuggestion(ctx, item.ID, "ready", resp.Suggestion); err != nil {
			r.Log.Warn().Err(err).Str("item", item.ItemName).Msg("store suggestion failed")
			continue
		}

		// Send notification
		msg := buildSuggestionMessage(item, resp.Suggestion)
		if err := r.sendSuggestionNotification(msg); err != nil {
			r.Log.Warn().Err(err).Str("item", item.ItemName).Msg("send suggestion notification failed")
			continue
		}

		r.Log.Info().
			Str("item", item.ItemName).
			Str("model", resp.Model).
			Int("tokens", resp.TokensUsed).
			Msg("AI suggestion sent")
	}

	return nil
}

func buildSuggestionMessage(item notify.PendingNotification, suggestion string) string {
	label := notificationLabel(item.EventType)
	msg := fmt.Sprintf(
		"💡 AI Suggestion Ready!\n\nMata Kuliah: %s\nItem: %s (%s)",
		item.CourseName,
		item.ItemName,
		label,
	)
	if item.DueDate != "" {
		msg += fmt.Sprintf("\nDue: %s", item.DueDate)
	}

	// Truncate suggestion if too long for WhatsApp
	s := strings.TrimSpace(suggestion)
	if len(s) > 1500 {
		s = s[:1500] + "..."
	}

	msg += fmt.Sprintf("\n\nSuggestion:\n%s", s)
	msg += fmt.Sprintf("\n\nLink: %s", item.ItemLink)
	return msg
}

func (r *Runner) sendSuggestionNotification(message string) error {
	if r.Dry {
		r.Log.Info().Str("message_preview", message[:min(200, len(message))]).Msg("dry-run: would send suggestion")
		return nil
	}

	targets := make([]notify.GroupMessage, 0, 1)
	// Suggestions only go to personal chat, not group
	if strings.TrimSpace(r.WAMe) != "" {
		targets = append(targets, notify.GroupMessage{Message: message, GroupID: r.WAMe})
	}
	if len(targets) == 0 {
		return nil
	}

	return notify.SendWhatsAppReliable(targets)
}
