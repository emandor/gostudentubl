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
