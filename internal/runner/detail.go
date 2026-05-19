package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/emandor/gostudentubl/internal/moodle"
	"github.com/emandor/gostudentubl/internal/notify"
)

func (r *Runner) fetchDetailsForPendingItems(ctx context.Context) error {
	if r.NotificationStore == nil || r.M == nil || !r.DetailFetchEnabled {
		return nil
	}
	limit := r.DetailFetchLimit
	if limit <= 0 {
		limit = 10
	}
	items, err := r.NotificationStore.ListNeedingDetail(ctx, limit)
	if err != nil {
		return fmt.Errorf("list items needing detail: %w", err)
	}
	if len(items) == 0 {
		return nil
	}

	for _, item := range items {
		if err := r.Limiter.Wait(ctx); err != nil {
			return err
		}

		switch item.EventType {
		case notify.NotificationTypeAssignment:
			detail, err := r.M.GetAssignmentDetail(ctx, item.ItemID)
			if err != nil {
				r.Log.Warn().Err(err).Int64("notification_id", item.ID).Str("item_id", item.ItemID).Msg("fetch assignment detail failed")
				continue
			}
			record := notify.ItemDetailRecord{
				EventID:          item.ID,
				EventType:        item.EventType,
				ItemID:           item.ItemID,
				SubmissionStatus: strings.TrimSpace(detail.SubmissionStatus),
				GradingStatus:    strings.TrimSpace(detail.GradingStatus),
				DueDate:          strings.TrimSpace(detail.DueDate),
				DueDateParsed:    parseDueDateRFC3339(detail.DueDate),
				FileSubmissions:  detail.FileSubmissions,
				RawContent:       assignmentRawContent(item, detail),
			}
			if err := r.NotificationStore.UpsertItemDetail(ctx, record); err != nil {
				r.Log.Warn().Err(err).Int64("notification_id", item.ID).Str("item_id", item.ItemID).Msg("store assignment detail failed")
				continue
			}
			r.Log.Info().Int64("notification_id", item.ID).Str("item_id", item.ItemID).Msg("assignment detail fetched")

		case notify.NotificationTypeQuiz:
			detail, err := r.M.GetQuizDetail(ctx, item.ItemID)
			if err != nil {
				r.Log.Warn().Err(err).Int64("notification_id", item.ID).Str("item_id", item.ItemID).Msg("fetch quiz detail failed")
				continue
			}
			record := notify.ItemDetailRecord{
				EventID:         item.ID,
				EventType:       item.EventType,
				ItemID:          item.ItemID,
				DueDate:         strings.TrimSpace(detail.CloseDate),
				DueDateParsed:   parseDueDateRFC3339(detail.CloseDate),
				AttemptsAllowed: strings.TrimSpace(detail.AttemptsAllowed),
				AttemptState:    strings.TrimSpace(detail.AttemptState),
				AttemptGrade:    strings.TrimSpace(detail.AttemptGrade),
				CanAttempt:      detail.CanAttempt,
				NoMoreAttempts:  detail.NoMoreAttempts,
				RawContent:      quizRawContent(item, detail),
			}
			if err := r.NotificationStore.UpsertItemDetail(ctx, record); err != nil {
				r.Log.Warn().Err(err).Int64("notification_id", item.ID).Str("item_id", item.ItemID).Msg("store quiz detail failed")
				continue
			}
			r.Log.Info().Int64("notification_id", item.ID).Str("item_id", item.ItemID).Msg("quiz detail fetched")
		}
	}

	return nil
}

func assignmentRawContent(item notify.PendingNotification, detail moodle.AssignmentDetail) string {
	parts := []string{
		"Type: assignment",
		"Course: " + item.CourseName,
		"Topic: " + item.ItemTitle,
		"Item: " + item.ItemName,
	}
	if strings.TrimSpace(detail.Description) != "" {
		parts = append(parts, "Description:\n"+strings.TrimSpace(detail.Description))
	}
	if strings.TrimSpace(detail.SubmissionStatus) != "" {
		parts = append(parts, "Submission Status: "+detail.SubmissionStatus)
	}
	if strings.TrimSpace(detail.GradingStatus) != "" {
		parts = append(parts, "Grading Status: "+detail.GradingStatus)
	}
	if strings.TrimSpace(detail.DueDate) != "" {
		parts = append(parts, "Due Date: "+detail.DueDate)
	}
	if strings.TrimSpace(detail.TimeRemaining) != "" {
		parts = append(parts, "Time Remaining: "+detail.TimeRemaining)
	}
	if strings.TrimSpace(detail.LastModified) != "" {
		parts = append(parts, "Last Modified: "+detail.LastModified)
	}
	if len(detail.FileSubmissions) > 0 {
		parts = append(parts, "File Submissions: "+strings.Join(detail.FileSubmissions, ", "))
	}
	return strings.Join(parts, "\n")
}

func quizRawContent(item notify.PendingNotification, detail moodle.QuizDetail) string {
	parts := []string{
		"Type: quiz",
		"Course: " + item.CourseName,
		"Topic: " + item.ItemTitle,
		"Item: " + item.ItemName,
	}
	if strings.TrimSpace(detail.AttemptsAllowed) != "" {
		parts = append(parts, "Attempts Allowed: "+detail.AttemptsAllowed)
	}
	if strings.TrimSpace(detail.CloseDate) != "" {
		parts = append(parts, "Close Date: "+detail.CloseDate)
	}
	if strings.TrimSpace(detail.AttemptState) != "" {
		parts = append(parts, "Attempt State: "+detail.AttemptState)
	}
	if strings.TrimSpace(detail.AttemptGrade) != "" {
		parts = append(parts, "Attempt Grade: "+detail.AttemptGrade)
	}
	if strings.TrimSpace(detail.AttemptDate) != "" {
		parts = append(parts, "Attempt Date: "+detail.AttemptDate)
	}
	parts = append(parts, fmt.Sprintf("Can Attempt: %t", detail.CanAttempt))
	parts = append(parts, fmt.Sprintf("No More Attempts: %t", detail.NoMoreAttempts))
	return strings.Join(parts, "\n")
}
