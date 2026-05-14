package notify

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestNotificationStoreLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := NewNotificationStore(filepath.Join(t.TempDir(), "notifications.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	events := []NotificationEvent{
		{
			EventType:  NotificationTypeAssignment,
			CourseID:   31160,
			CourseName: "Pemrograman",
			ItemTitle:  "Topik 1",
			ItemName:   "Assignment 1",
			ItemLink:   "https://elearning.example.com/mod/assign/view.php?id=1001",
			ItemID:     "1001",
		},
		{
			EventType:  NotificationTypeQuiz,
			CourseID:   31160,
			CourseName: "Pemrograman",
			ItemTitle:  "Topik 2",
			ItemName:   "Quiz 1",
			ItemLink:   "https://elearning.example.com/mod/quiz/view.php?id=2001",
			ItemID:     "2001",
		},
	}

	if err := store.UpsertEvents(ctx, events); err != nil {
		t.Fatalf("upsert events: %v", err)
	}

	pending, err := store.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}

	if err := store.MarkNotified(ctx, pending[0].ID, time.Now()); err != nil {
		t.Fatalf("mark notified: %v", err)
	}

	pending, err = store.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending after mark notified: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending after mark notified, got %d", len(pending))
	}

	if err := store.UpsertEvents(ctx, []NotificationEvent{events[0]}); err != nil {
		t.Fatalf("upsert duplicate event: %v", err)
	}

	pending, err = store.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending after duplicate upsert: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected still 1 pending after duplicate upsert, got %d", len(pending))
	}

	if err := store.MarkFailed(ctx, pending[0].ID, errors.New("send failed")); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	pending, err = store.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending after mark failed: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending after mark failed, got %d", len(pending))
	}
	if pending[0].AttemptCount != 1 {
		t.Fatalf("expected attempt_count=1, got %d", pending[0].AttemptCount)
	}
	if pending[0].LastError == "" {
		t.Fatalf("expected last_error to be set")
	}
}

func TestNotificationStorePrune(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := NewNotificationStore(filepath.Join(t.TempDir(), "notifications.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	event := NotificationEvent{
		EventType:  NotificationTypeAssignment,
		CourseID:   31160,
		CourseName: "Pemrograman",
		ItemTitle:  "Topik",
		ItemName:   "Assignment",
		ItemLink:   "https://elearning.example.com/mod/assign/view.php?id=1001",
		ItemID:     "1001",
	}

	if err := store.UpsertEvents(ctx, []NotificationEvent{event}); err != nil {
		t.Fatalf("upsert event: %v", err)
	}

	pending, err := store.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}

	oldTime := time.Now().AddDate(0, 0, -45)
	if err := store.MarkNotified(ctx, pending[0].ID, oldTime); err != nil {
		t.Fatalf("mark notified: %v", err)
	}

	deleted, err := store.PruneNotifiedBefore(ctx, time.Now().AddDate(0, 0, -30))
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted record, got %d", deleted)
	}
}

func TestNotificationStoreApproachingDeadlinesAndReminderFlags(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := NewNotificationStore(filepath.Join(t.TempDir(), "notifications.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().UTC()
	events := []NotificationEvent{
		{
			EventType:            NotificationTypeAssignment,
			CourseID:             1,
			CourseName:           "Pemrograman",
			ItemTitle:            "Topik 1",
			ItemName:             "Tugas 1",
			ItemLink:             "https://elearning.example.com/mod/assign/view.php?id=301",
			ItemID:               "301",
			SubmissionStatus:     "No submission",
			DueDateParsedRFC3339: now.Add(8 * time.Hour).Format(time.RFC3339),
		},
		{
			EventType:            NotificationTypeAssignment,
			CourseID:             1,
			CourseName:           "Pemrograman",
			ItemTitle:            "Topik 2",
			ItemName:             "Tugas 2",
			ItemLink:             "https://elearning.example.com/mod/assign/view.php?id=302",
			ItemID:               "302",
			SubmissionStatus:     "Submitted for grading",
			DueDateParsedRFC3339: now.Add(7 * time.Hour).Format(time.RFC3339),
		},
	}
	if err := store.UpsertEvents(ctx, events); err != nil {
		t.Fatalf("upsert events: %v", err)
	}

	items, err := store.ListApproachingDeadlines(ctx, now, 24*time.Hour)
	if err != nil {
		t.Fatalf("list approaching deadlines: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one approaching deadline, got %d", len(items))
	}

	if err := store.MarkReminder24hSent(ctx, items[0].ID); err != nil {
		t.Fatalf("mark reminder 24h: %v", err)
	}
	if err := store.MarkReminder12hSent(ctx, items[0].ID); err != nil {
		t.Fatalf("mark reminder 12h: %v", err)
	}

	pending, err := store.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) == 0 || !pending[0].Reminder24hSent || !pending[0].Reminder12hSent {
		t.Fatalf("expected reminder flags set on pending row, got %+v", pending)
	}
}

func TestNotificationStoreSuggestionQueries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := NewNotificationStore(filepath.Join(t.TempDir(), "notifications.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	event := NotificationEvent{
		EventType:        NotificationTypeQuiz,
		CourseID:         1,
		CourseName:       "Pemrograman",
		ItemTitle:        "Topik Quiz",
		ItemName:         "Quiz 1",
		ItemLink:         "https://elearning.example.com/mod/quiz/view.php?id=401",
		ItemID:           "401",
		SubmissionStatus: "",
	}
	if err := store.UpsertEvents(ctx, []NotificationEvent{event}); err != nil {
		t.Fatalf("upsert event: %v", err)
	}

	pending, err := store.ListPending(ctx, 1)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}

	if err := store.UpsertItemDetail(ctx, ItemDetailRecord{
		EventID:      pending[0].ID,
		EventType:    event.EventType,
		ItemID:       event.ItemID,
		RawContent:   "quiz content for ai",
		CanAttempt:   true,
		FetchedAt:    time.Now(),
		AttemptState: "Finished",
	}); err != nil {
		t.Fatalf("upsert item detail: %v", err)
	}

	items, err := store.ListNeedingSuggestion(ctx, 10)
	if err != nil {
		t.Fatalf("list needing suggestion: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item needing suggestion, got %d", len(items))
	}

	if err := store.UpdateSuggestion(ctx, items[0].ID, "ready", "answer"); err != nil {
		t.Fatalf("update suggestion: %v", err)
	}

	items, err = store.ListNeedingSuggestion(ctx, 10)
	if err != nil {
		t.Fatalf("list needing suggestion after update: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no suggestion-pending item after ready update, got %d", len(items))
	}
}
