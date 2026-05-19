package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/emandor/gostudentubl/internal/notify"
	"github.com/emandor/gostudentubl/internal/solver"
)

func (r *Runner) processDrafts(ctx context.Context) error {
	return r.processDraftsWithStats(ctx, nil)
}

func (r *Runner) processDraftsWithStats(ctx context.Context, stats *runStats) error {
	if !r.DraftEnabled {
		return nil
	}

	items, err := r.NotificationStore.ListNeedingDraft(ctx, r.DraftLimit)
	if err != nil {
		return fmt.Errorf("list needing draft: %w", err)
	}
	if len(items) == 0 {
		return nil
	}

	r.Log.Info().Int("items_needing_draft", len(items)).Msg("processing AI drafts")

	for _, item := range items {
		if err := r.NotificationStore.UpdateDraft(ctx, item.ID, "generating", "", "", ""); err != nil {
			r.Log.Warn().Err(err).Str("item", item.ItemName).Msg("mark draft generating failed")
			continue
		}

		req := solver.SolveRequest{
			EventType:  item.EventType,
			CourseName: item.CourseName,
			ItemTitle:  item.ItemTitle,
			ItemName:   item.ItemName,
			DueDate:    item.DueDate,
			RawContent: item.RawContent,
		}

		// MultiSolver path: run all providers concurrently.
		if r.MultiSolver != nil {
			if r.processItemMulti(ctx, item, req) && stats != nil {
				stats.DraftsReady++
			}
			continue
		}

		// Legacy single-solver path.
		resp, err := r.Solver.Solve(ctx, req)
		if err != nil {
			r.Log.Warn().Err(err).Str("item", item.ItemName).Msg("solver failed; resetting draft_status")
			if resetErr := r.NotificationStore.UpdateDraft(ctx, item.ID, "", "", "", ""); resetErr != nil {
				r.Log.Warn().Err(resetErr).Msg("reset draft status failed")
			}
			continue
		}

		if err := r.NotificationStore.UpdateDraft(ctx, item.ID, "ready", resp.DraftText, resp.Provider, resp.Model); err != nil {
			r.Log.Warn().Err(err).Str("item", item.ItemName).Msg("store draft failed")
			continue
		}
		if stats != nil {
			stats.DraftsReady++
		}

		msg := buildDraftReadyMessage(item, resp.Provider)
		if err := r.sendDraftNotification(msg); err != nil {
			r.Log.Warn().Err(err).Str("item", item.ItemName).Msg("send draft notification failed")
		}

		r.Log.Info().
			Str("item", item.ItemName).
			Str("provider", resp.Provider).
			Str("model", resp.Model).
			Int("tokens", resp.TokensUsed).
			Msg("draft ready")
	}

	return nil
}

// processItemMulti runs all providers concurrently, stores each result, then
// marks the item ready if at least one provider succeeded.
func (r *Runner) processItemMulti(ctx context.Context, item notify.NotificationEvent, req solver.SolveRequest) bool {
	results := r.MultiSolver.SolveAll(ctx, req)

	var successCount int
	for _, res := range results {
		errStr := ""
		if res.Error != nil {
			errStr = res.Error.Error()
		}
		if err := r.NotificationStore.UpsertDraftResult(ctx, item.ID, res.Provider, res.Model, res.DraftText, res.TokensUsed, errStr, res.Attempts); err != nil {
			r.Log.Warn().Err(err).Str("provider", res.Provider).Str("item", item.ItemName).Msg("store provider result failed")
		}
		if res.Error == nil && strings.TrimSpace(res.DraftText) != "" {
			successCount++
		}
		logEvent := r.Log.Info().Str("item", item.ItemName).Str("provider", res.Provider).Int("attempts", res.Attempts)
		if res.Error != nil {
			logEvent.Err(res.Error).Msg("provider failed")
		} else {
			logEvent.Str("model", res.Model).Int("tokens", res.TokensUsed).Msg("provider succeeded")
		}
	}

	if successCount == 0 {
		r.Log.Warn().Str("item", item.ItemName).Msg("all providers failed; resetting draft_status")
		if resetErr := r.NotificationStore.UpdateDraft(ctx, item.ID, "", "", "", ""); resetErr != nil {
			r.Log.Warn().Err(resetErr).Msg("reset draft status failed")
		}
		return false
	}

	best := solver.PickBest(results)
	if best == nil {
		best = &results[0]
	}

	if err := r.NotificationStore.UpdateDraft(ctx, item.ID, "ready", best.DraftText, best.Provider, best.Model); err != nil {
		r.Log.Warn().Err(err).Str("item", item.ItemName).Msg("store draft failed")
		return false
	}

	providers := make([]string, 0, len(results))
	for _, res := range results {
		if res.Error == nil {
			providers = append(providers, res.Provider)
		}
	}
	msg := buildDraftReadyMessage(item, strings.Join(providers, ", "))
	if err := r.sendDraftNotification(msg); err != nil {
		r.Log.Warn().Err(err).Str("item", item.ItemName).Msg("send draft notification failed")
	}
	return true
}

func buildDraftReadyMessage(item notify.NotificationEvent, provider string) string {
	due := formatWIB(item.DueDateParsedRFC3339)
	if due == "-" {
		due = strings.TrimSpace(item.DueDate)
		if due == "" {
			due = "-"
		}
	}
	return fmt.Sprintf(
		"📝 Draft siap untuk direview!\n\nMata Kuliah: %s\nItem: %s\nDue: %s\nProvider: %s\n\n🔗 https://draft.arisjirat.com",
		item.CourseName,
		item.ItemName,
		due,
		provider,
	)
}

func (r *Runner) sendDraftNotification(message string) error {
	if r.Dry {
		r.Log.Info().Str("message_preview", message[:min(200, len(message))]).Msg("dry-run: would send draft notification")
		return nil
	}
	if strings.TrimSpace(r.WAMe) == "" {
		return nil
	}
	return notify.SendWhatsAppReliable([]notify.GroupMessage{
		{Message: message, GroupID: r.WAMe},
	})
}
