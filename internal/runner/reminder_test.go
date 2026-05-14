package runner

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/time/rate"

	"github.com/emandor/gostudentubl/internal/notify"
)

func newTestStore(t *testing.T) *notify.NotificationStore {
	t.Helper()
	store, err := notify.NewNotificationStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestCheckDeadlineReminders24h(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newTestStore(t)

	// Insert an event with due date 20 hours from now
	now := time.Now().UTC()
	dueDate := now.Add(20 * time.Hour)

	events := []notify.NotificationEvent{{
		EventType:        notify.NotificationTypeAssignment,
		CourseID:         1,
		CourseName:       "Test Course",
		ItemTitle:        "Topic",
		ItemName:         "Assignment Due Soon",
		ItemLink:         "https://example.com/assign/1",
		ItemID:           "1",
		DueDate:          "Saturday, 28 February 2026, 11:59 PM",
		SubmissionStatus: "No submission",
		DueDateParsedRFC3339:    dueDate.Format(time.RFC3339),
	}}
	if err := store.UpsertEvents(ctx, events); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	r := &Runner{
		Log:               zerolog.Nop(),
		Limiter:           rate.NewLimiter(10, 10),
		NotificationStore: store,
		Dry:               true, // dry run to avoid actual WA sends
	}

	if err := r.checkDeadlineReminders(ctx, now); err != nil {
		t.Fatalf("checkDeadlineReminders: %v", err)
	}

	// Verify the 24h reminder was marked
	items, err := store.ListApproachingDeadlines(ctx, now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Reminder24hSent != true {
		t.Fatalf("expected Reminder24hSent=1, got %v", items[0].Reminder24hSent)
	}
}

func TestCheckDeadlineReminders12h(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newTestStore(t)

	now := time.Now().UTC()
	dueDate := now.Add(8 * time.Hour) // 8h from now → should trigger 12h reminder

	events := []notify.NotificationEvent{{
		EventType:        notify.NotificationTypeQuiz,
		CourseID:         1,
		CourseName:       "Test",
		ItemTitle:        "Topic",
		ItemName:         "Quiz Due Very Soon",
		ItemLink:         "https://example.com/quiz/1",
		ItemID:           "2",
		DueDate:          "Monday, 30 March 2026, 11:59 PM",
		SubmissionStatus: "No submission",
		DueDateParsedRFC3339:    dueDate.Format(time.RFC3339),
	}}
	if err := store.UpsertEvents(ctx, events); err != nil {
		t.Fatal(err)
	}

	r := &Runner{
		Log:               zerolog.Nop(),
		Limiter:           rate.NewLimiter(10, 10),
		NotificationStore: store,
		Dry:               true,
	}

	if err := r.checkDeadlineReminders(ctx, now); err != nil {
		t.Fatal(err)
	}

	items, err := store.ListApproachingDeadlines(ctx, now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Reminder12hSent != true {
		t.Fatalf("expected Reminder12hSent=1, got %v", items[0].Reminder12hSent)
	}
}

func TestNoReminderForSubmittedItems(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newTestStore(t)

	now := time.Now().UTC()
	dueDate := now.Add(8 * time.Hour)

	events := []notify.NotificationEvent{{
		EventType:        notify.NotificationTypeAssignment,
		CourseID:         1,
		CourseName:       "Test",
		ItemTitle:        "Topic",
		ItemName:         "Submitted Assignment",
		ItemLink:         "https://example.com/assign/3",
		ItemID:           "3",
		DueDate:          "Monday, 30 March 2026, 11:59 PM",
		SubmissionStatus: "Submitted for grading",
		DueDateParsedRFC3339:    dueDate.Format(time.RFC3339),
	}}
	if err := store.UpsertEvents(ctx, events); err != nil {
		t.Fatal(err)
	}

	// Submitted items should not appear in approaching deadlines
	items, err := store.ListApproachingDeadlines(ctx, now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items for submitted assignment, got %d", len(items))
	}
}

func TestNoReminderResent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newTestStore(t)

	now := time.Now().UTC()
	dueDate := now.Add(20 * time.Hour)

	events := []notify.NotificationEvent{{
		EventType:     notify.NotificationTypeAssignment,
		CourseID:      1,
		CourseName:    "Test",
		ItemTitle:     "Topic",
		ItemName:      "Assignment",
		ItemLink:      "https://example.com/assign/4",
		ItemID:        "4",
		DueDate:       "Monday, 30 March 2026, 11:59 PM",
		DueDateParsedRFC3339: dueDate.Format(time.RFC3339),
	}}
	if err := store.UpsertEvents(ctx, events); err != nil {
		t.Fatal(err)
	}

	r := &Runner{
		Log:               zerolog.Nop(),
		Limiter:           rate.NewLimiter(10, 10),
		NotificationStore: store,
		Dry:               true,
	}

	// First call sends reminder
	if err := r.checkDeadlineReminders(ctx, now); err != nil {
		t.Fatal(err)
	}

	// Second call should NOT re-send (flag already set)
	if err := r.checkDeadlineReminders(ctx, now); err != nil {
		t.Fatal(err)
	}

	// Verify reminder was only sent once (flag=1 not 2)
	items, err := store.ListApproachingDeadlines(ctx, now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 1 && items[0].Reminder24hSent != true {
		t.Fatalf("expected Reminder24hSent=1, got %v", items[0].Reminder24hSent)
	}
}
