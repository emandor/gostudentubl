package runner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emandor/gostudentubl/internal/notify"
)

func (r *Runner) SendDailySummary(ctx context.Context) error {
	if r.NotificationStore == nil {
		return nil
	}
	loc, err := time.LoadLocation(r.Timezone)
	if err != nil {
		return fmt.Errorf("timezone: %w", err)
	}
	now := time.Now().In(loc)
	since := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	summary, err := r.NotificationStore.DailyOperationalSummary(ctx, since, now)
	if err != nil {
		return err
	}
	message := buildDailySummaryMessage(summary, loc)

	if r.Dry {
		r.Log.Info().Str("message", message).Msg("dry-run: would send daily summary")
		return nil
	}

	targets := make([]notify.GroupMessage, 0, 1)
	if strings.TrimSpace(r.WAMe) != "" {
		targets = append(targets, notify.GroupMessage{Message: message, GroupID: r.WAMe})
	} else if strings.TrimSpace(r.WAGroup) != "" {
		targets = append(targets, notify.GroupMessage{Message: "🤖 " + message, GroupID: r.WAGroup})
	}
	if len(targets) == 0 {
		return nil
	}
	return r.sendReliable(targets)
}

func buildDailySummaryMessage(summary notify.OperationalSummary, loc *time.Location) string {
	status := "✅ OK"
	if summary.RunFailed > 0 {
		status = "⚠️ Ada failure"
	}
	lines := []string{
		"📊 Daily Moodle Bot Summary",
		"",
		fmt.Sprintf("Status: %s", status),
		fmt.Sprintf("Window: %s – %s", summary.Since.In(loc).Format("02 Jan 15:04"), summary.Until.In(loc).Format("15:04")),
		"",
		fmt.Sprintf("Runs: %d success / %d failed / %d total", summary.RunSuccess, summary.RunFailed, summary.RunTotal),
		fmt.Sprintf("Attendance submitted: %d", summary.AttendanceSubmitted),
		fmt.Sprintf("Assignments scanned: %d", summary.AssignmentsFound),
		fmt.Sprintf("Quizzes scanned: %d", summary.QuizzesFound),
		fmt.Sprintf("Notifications sent: %d", summary.NotificationsSent),
		fmt.Sprintf("Reminders sent: %d", summary.RemindersSent),
		fmt.Sprintf("Drafts ready: %d", summary.DraftsReady),
		"",
		fmt.Sprintf("Pending notifications: %d", summary.PendingNotifications),
		fmt.Sprintf("Ready drafts: %d", summary.ReadyDrafts),
		fmt.Sprintf("Open drafts: %d", summary.OpenDrafts),
	}
	if strings.TrimSpace(summary.LastError) != "" {
		lines = append(lines, "", "Last error:", truncateSummaryLine(summary.LastError, 240))
	}
	return strings.Join(lines, "\n")
}

func truncateSummaryLine(s string, limit int) string {
	s = strings.TrimSpace(s)
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}
