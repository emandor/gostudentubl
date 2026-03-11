package runner

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/emandor/gostudentubl/internal/notify"
)

func (r *Runner) fetchItemDetails(ctx context.Context) error {
	limit := r.DetailFetchLimit
	if limit <= 0 {
		limit = 10
	}

	items, err := r.NotificationStore.ListUnfetchedDetails(ctx, limit)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

	r.Log.Info().Int("unfetched_items", len(items)).Msg("fetching detail pages")

	for _, item := range items {
		if err := r.Limiter.Wait(ctx); err != nil {
			return err
		}

		switch item.EventType {
		case notify.NotificationTypeAssignment:
			detail, err := r.M.GetAssignmentDetail(ctx, item.ItemID)
			if err != nil {
				r.Log.Warn().Err(err).Str("item", item.ItemName).Msg("fetch assignment detail failed")
				continue
			}
			filesJSON, _ := json.Marshal(detail.FileSubmissions)
			if err := r.NotificationStore.UpsertItemDetail(
				ctx, item.ID, item.EventType, item.ItemID,
				detail.SubmissionStatus, detail.GradingStatus, detail.DueDate,
				"", "", "", false, false,
				string(filesJSON), buildAssignmentRawContent(item, detail),
			); err != nil {
				r.Log.Warn().Err(err).Str("item", item.ItemName).Msg("upsert assignment detail failed")
				continue
			}

		case notify.NotificationTypeQuiz:
			detail, err := r.M.GetQuizDetail(ctx, item.ItemID)
			if err != nil {
				r.Log.Warn().Err(err).Str("item", item.ItemName).Msg("fetch quiz detail failed")
				continue
			}
			if err := r.NotificationStore.UpsertItemDetail(
				ctx, item.ID, item.EventType, item.ItemID,
				"", "", "",
				detail.AttemptsAllowed, detail.AttemptState, detail.AttemptGrade,
				detail.CanAttempt, detail.NoMoreAttempts,
				"", buildQuizRawContent(item, detail),
			); err != nil {
				r.Log.Warn().Err(err).Str("item", item.ItemName).Msg("upsert quiz detail failed")
				continue
			}
		}

		if err := r.NotificationStore.UpdateDetailFetched(ctx, item.ID); err != nil {
			r.Log.Warn().Err(err).Int64("id", item.ID).Msg("mark detail fetched failed")
		}
		r.Log.Info().Str("type", item.EventType).Str("item", item.ItemName).Msg("detail page fetched")
	}

	return nil
}

func buildAssignmentRawContent(item notify.PendingNotification, detail interface{ }) string {
	type ad = struct {
		SubmissionStatus string
		GradingStatus    string
		DueDate          string
		TimeRemaining    string
		LastModified     string
		FileSubmissions  []string
	}
	var sb strings.Builder
	sb.WriteString("Assignment: " + item.ItemName + "\n")
	sb.WriteString("Course: " + item.CourseName + "\n")
	sb.WriteString("Topic: " + item.ItemTitle + "\n")
	if item.DueDate != "" {
		sb.WriteString("Due Date: " + item.DueDate + "\n")
	}
	sb.WriteString("Link: " + item.ItemLink + "\n")
	_ = ad{}
	return sb.String()
}

func buildQuizRawContent(item notify.PendingNotification, detail interface{}) string {
	var sb strings.Builder
	sb.WriteString("Quiz: " + item.ItemName + "\n")
	sb.WriteString("Course: " + item.CourseName + "\n")
	sb.WriteString("Topic: " + item.ItemTitle + "\n")
	if item.DueDate != "" {
		sb.WriteString("Close Date: " + item.DueDate + "\n")
	}
	sb.WriteString("Link: " + item.ItemLink + "\n")
	return sb.String()
}
