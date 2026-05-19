package runner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emandor/gostudentubl/internal/notify"
)

func (r *Runner) checkDeadlineReminders(ctx context.Context, now time.Time) error {
	return r.checkDeadlineRemindersWithStats(ctx, now, nil)
}

func (r *Runner) checkDeadlineRemindersWithStats(ctx context.Context, now time.Time, stats *runStats) error {
	// Query items approaching deadline within 24h window
	items, err := r.NotificationStore.ListApproachingDeadlines(ctx, now, 24*time.Hour)
	if err != nil {
		return fmt.Errorf("list approaching deadlines: %w", err)
	}
	if len(items) == 0 {
		return nil
	}

	r.Log.Info().Int("approaching_items", len(items)).Msg("checking deadline reminders")

	for _, item := range items {
		dueTime, err := time.Parse(time.RFC3339, item.DueDateParsed)
		if err != nil {
			r.Log.Warn().Err(err).Str("item", item.ItemName).Str("due", item.DueDateParsed).Msg("parse due date for reminder")
			continue
		}

		timeLeft := dueTime.Sub(now)

		// 12h reminder (higher priority, check first)
		if timeLeft <= 12*time.Hour && !item.Reminder12hSent {
			msg := buildReminderMessage(item, timeLeft, "12h")
			if err := r.sendReminderNotification(msg); err != nil {
				r.Log.Warn().Err(err).Str("item", item.ItemName).Msg("send 12h reminder failed")
				continue
			}
			if err := r.NotificationStore.MarkReminder12hSent(ctx, item.ID); err != nil {
				r.Log.Warn().Err(err).Str("item", item.ItemName).Msg("mark 12h reminder sent failed")
			}
			if stats != nil {
				stats.RemindersSent++
			}
			r.Log.Info().Str("item", item.ItemName).Dur("time_left", timeLeft).Msg("12h reminder sent")
			continue
		}

		// 24h reminder
		if timeLeft <= 24*time.Hour && !item.Reminder24hSent {
			msg := buildReminderMessage(item, timeLeft, "24h")
			if err := r.sendReminderNotification(msg); err != nil {
				r.Log.Warn().Err(err).Str("item", item.ItemName).Msg("send 24h reminder failed")
				continue
			}
			if err := r.NotificationStore.MarkReminder24hSent(ctx, item.ID); err != nil {
				r.Log.Warn().Err(err).Str("item", item.ItemName).Msg("mark 24h reminder sent failed")
			}
			if stats != nil {
				stats.RemindersSent++
			}
			r.Log.Info().Str("item", item.ItemName).Dur("time_left", timeLeft).Msg("24h reminder sent")
		}
	}

	return nil
}

func buildReminderMessage(item notify.PendingNotification, timeLeft time.Duration, window string) string {
	label := notificationLabel(item.EventType)
	hours := int(timeLeft.Hours())
	minutes := int(timeLeft.Minutes()) % 60

	status := item.SubmissionStatus
	if status == "" {
		status = "Belum dikerjakan"
	}

	return fmt.Sprintf(
		"⏰ Reminder: %s deadline approaching! (%s)\n\nMata Kuliah: %s\nItem: %s\nDue: %s\nTime Left: ~%dh %dm\nStatus: %s\nLink: %s",
		label,
		window,
		item.CourseName,
		item.ItemName,
		item.DueDate,
		hours,
		minutes,
		status,
		item.ItemLink,
	)
}

func (r *Runner) sendReminderNotification(message string) error {
	if r.Dry {
		r.Log.Info().Str("message", message).Msg("dry-run: would send reminder")
		return nil
	}

	targets := make([]notify.GroupMessage, 0, 2)
	if strings.TrimSpace(r.WAMe) != "" {
		targets = append(targets, notify.GroupMessage{Message: message, GroupID: r.WAMe})
	}
	if strings.TrimSpace(r.WAGroup) != "" {
		targets = append(targets, notify.GroupMessage{Message: "🤖 " + message, GroupID: r.WAGroup})
	}
	if len(targets) == 0 {
		return nil
	}

	return notify.SendWhatsAppReliable(targets)
}
