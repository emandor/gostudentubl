package notify

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	NotificationTypeAssignment = "assignment"
	NotificationTypeQuiz       = "quiz"
)

type NotificationEvent struct {
	EventType  string
	CourseID   int
	CourseName string
	ItemTitle  string
	ItemName   string
	ItemLink   string
	ItemID     string
}

type PendingNotification struct {
	ID           int64
	EventType    string
	CourseID     int
	CourseName   string
	ItemTitle    string
	ItemName     string
	ItemLink     string
	ItemID       string
	AttemptCount int
	LastError    string
}

type NotificationStore struct {
	db *sql.DB
}

func NewNotificationStore(dbPath string) (*NotificationStore, error) {
	if strings.TrimSpace(dbPath) == "" {
		dbPath = "notifications.db"
	}
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir db dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	store := &NotificationStore{db: db}
	if err := store.init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *NotificationStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *NotificationStore) init(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `PRAGMA journal_mode=WAL;`); err != nil {
		return fmt.Errorf("sqlite pragma journal_mode: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA busy_timeout=5000;`); err != nil {
		return fmt.Errorf("sqlite pragma busy_timeout: %w", err)
	}
	const q = `
CREATE TABLE IF NOT EXISTS notification_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	event_type TEXT NOT NULL,
	course_id INTEGER NOT NULL,
	course_name TEXT NOT NULL,
	item_title TEXT NOT NULL,
	item_name TEXT NOT NULL,
	item_link TEXT NOT NULL,
	item_id TEXT NOT NULL,
	fingerprint TEXT NOT NULL UNIQUE,
	notified INTEGER NOT NULL DEFAULT 0 CHECK(notified IN (0, 1)),
	attempt_count INTEGER NOT NULL DEFAULT 0,
	last_error TEXT NOT NULL DEFAULT '',
	first_seen_at TEXT NOT NULL,
	notified_at TEXT,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_notification_events_notified_id ON notification_events(notified, id);
`
	if _, err := s.db.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("migrate notification_events: %w", err)
	}
	return nil
}

func (s *NotificationStore) UpsertEvents(ctx context.Context, events []NotificationEvent) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO notification_events (
	event_type, course_id, course_name, item_title, item_name, item_link, item_id,
	fingerprint, notified, attempt_count, last_error, first_seen_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, 0, '', ?, ?)
ON CONFLICT(fingerprint) DO UPDATE SET
	course_name = excluded.course_name,
	item_title = excluded.item_title,
	item_name = excluded.item_name,
	item_link = excluded.item_link,
	updated_at = excluded.updated_at;
`)
	if err != nil {
		return fmt.Errorf("prepare upsert: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, ev := range events {
		if err := validateEvent(ev); err != nil {
			return err
		}
		fp := fingerprintForEvent(ev)
		if _, err := stmt.ExecContext(
			ctx,
			ev.EventType,
			ev.CourseID,
			ev.CourseName,
			ev.ItemTitle,
			ev.ItemName,
			ev.ItemLink,
			ev.ItemID,
			fp,
			now,
			now,
		); err != nil {
			return fmt.Errorf("upsert event: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit upsert tx: %w", err)
	}
	return nil
}

func (s *NotificationStore) ListPending(ctx context.Context, limit int) ([]PendingNotification, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, event_type, course_id, course_name, item_title, item_name, item_link, item_id, attempt_count, last_error
FROM notification_events
WHERE notified = 0
ORDER BY id ASC
LIMIT ?;
`, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending: %w", err)
	}
	defer rows.Close()

	out := make([]PendingNotification, 0, limit)
	for rows.Next() {
		var item PendingNotification
		if err := rows.Scan(
			&item.ID,
			&item.EventType,
			&item.CourseID,
			&item.CourseName,
			&item.ItemTitle,
			&item.ItemName,
			&item.ItemLink,
			&item.ItemID,
			&item.AttemptCount,
			&item.LastError,
		); err != nil {
			return nil, fmt.Errorf("scan pending: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending: %w", err)
	}
	return out, nil
}

func (s *NotificationStore) MarkNotified(ctx context.Context, id int64, at time.Time) error {
	notifiedAt := at.UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `
UPDATE notification_events
SET notified = 1, notified_at = ?, last_error = '', updated_at = ?
WHERE id = ?;
`, notifiedAt, notifiedAt, id); err != nil {
		return fmt.Errorf("mark notified: %w", err)
	}
	return nil
}

func (s *NotificationStore) MarkFailed(ctx context.Context, id int64, sendErr error) error {
	msg := "unknown error"
	if sendErr != nil {
		msg = sendErr.Error()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `
UPDATE notification_events
SET attempt_count = attempt_count + 1, last_error = ?, updated_at = ?
WHERE id = ?;
`, msg, now, id); err != nil {
		return fmt.Errorf("mark failed: %w", err)
	}
	return nil
}

func (s *NotificationStore) PruneNotifiedBefore(ctx context.Context, olderThan time.Time) (int64, error) {
	cutoff := olderThan.UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `
DELETE FROM notification_events
WHERE notified = 1 AND notified_at IS NOT NULL AND notified_at < ?;
`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune notified: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune rows affected: %w", err)
	}
	return rows, nil
}

func validateEvent(ev NotificationEvent) error {
	if ev.EventType != NotificationTypeAssignment && ev.EventType != NotificationTypeQuiz {
		return fmt.Errorf("unsupported event type: %q", ev.EventType)
	}
	if ev.CourseID <= 0 {
		return errors.New("course id must be > 0")
	}
	if strings.TrimSpace(ev.CourseName) == "" {
		return errors.New("course name is required")
	}
	if strings.TrimSpace(ev.ItemName) == "" {
		return errors.New("item name is required")
	}
	if strings.TrimSpace(ev.ItemLink) == "" {
		return errors.New("item link is required")
	}
	if strings.TrimSpace(ev.ItemID) == "" {
		return errors.New("item id is required")
	}
	return nil
}

func fingerprintForEvent(ev NotificationEvent) string {
	return fmt.Sprintf("%s:%d:%s", ev.EventType, ev.CourseID, strings.TrimSpace(ev.ItemID))
}
