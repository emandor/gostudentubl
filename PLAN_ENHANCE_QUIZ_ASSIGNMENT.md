# Plan: Enhance Quiz & Assignment System

## Problem Statement

The current system detects quiz/assignment items and sends a "new item found" WhatsApp notification but:

1. **No due date / close date** is parsed from list pages (the HTML has them but parser ignores them)
2. **No submission status** tracking ("Submitted for grading" vs "No submission")
3. **No deadline reminders** (24h / 12h before due)
4. **No detail page parsing** (richer data: attempt history, file submissions, grading status)
5. **No LLM suggestion system** to help tackle quizzes/assignments
6. **No grade tracking** from list pages

## Current State Analysis

### What exists and works

- `parseAssignmentList()` / `parseQuizList()` in `parser.go` — extract ID, title, name, link only
- `notification_events` SQLite table — fingerprint dedup, notified flag, attempt tracking
- `fetchAssignmentsAndQuizzes()` in `runner.go` — upsert events, send "new item" WA notifications
- WhatsApp sender (`notify/whatsapp.go`) — reliable + concurrent modes
- Period filtering — auto/manual/legacy modes

### HTML fields available but NOT parsed

**Assignment list page** (`/mod/assign/index.php?id=COURSE_ID`):

- `td.cell.c2` → Due date (e.g., "Saturday, 28 February 2026, 11:59 PM")
- `td.cell.c3` → Submission status ("Submitted for grading" / "No submission")
- `td.cell.c4` → Grade ("-" or numeric)

**Quiz list page** (`/mod/quiz/index.php?id=COURSE_ID`):

- `td.cell.c2` → Quiz closes date (e.g., "Monday, 30 March 2026, 11:59 PM")
- `td.cell.c3` → Grade ("100.00/100.00" or empty)

**Assignment detail page** (`/mod/assign/view.php?id=ITEM_ID`):

- Submission status, Grading status, Due date, Time remaining
- File submissions list, Last modified date
- Comments count

**Quiz detail page** (`/mod/quiz/view.php?id=ITEM_ID`):

- Attempts allowed, Quiz close date
- Previous attempt state, grade, submission date
- "No more attempts" or "Attempt quiz now" button

## Implementation Plan

---

### Phase 1: Enhanced Data Model & Parsing

**Goal:** Parse due dates, submission status, and grades from list pages. Add new fields to structs and SQLite schema.

#### 1A. Extend Moodle structs (`internal/moodle/client.go`)

```go
type Assignment struct {
    Title            string
    AssignmentName   string
    AssignmentLink   string
    AssignmentID     string
    DueDate          string  // raw text from HTML, e.g. "Saturday, 28 February 2026, 11:59 PM"
    SubmissionStatus string  // "Submitted for grading" / "No submission" / ""
    Grade            string  // "-" / "85" / ""
    Course           Course
}

type Quiz struct {
    Title      string
    QuizName   string
    QuizLink   string
    QuizID     string
    CloseDate  string  // raw text, e.g. "Monday, 30 March 2026, 11:59 PM"
    Grade      string  // "100.00/100.00" / ""
    Course     Course
}
```

#### 1B. Update parsers (`internal/moodle/parser.go`)

Update `parseAssignmentList()`:

- Extract `td.cell.c2` → `DueDate`
- Extract `td.cell.c3` → `SubmissionStatus`
- Extract `td.cell.c4` → `Grade`

Update `parseQuizList()`:

- Extract `td.cell.c2` → `CloseDate`
- Extract `td.cell.c3` → `Grade`

Add date parser function:

```go
// parseMoodleDate parses "Saturday, 28 February 2026, 11:59 PM" → time.Time
func parseMoodleDate(raw string) (time.Time, error)
```

Moodle date format: `"Monday, 2 January 2006, 3:04 PM"` (Go reference time format).

#### 1C. Add detail page parsers (`internal/moodle/parser.go`)

New functions:

- `parseAssignmentDetail(doc) → AssignmentDetail`
  - Extract from `.submissionsummarytable`: submission status, grading status, due date, time remaining, last modified, file submissions
- `parseQuizDetail(doc) → QuizDetail`
  - Extract from `.quizinfo`: attempts allowed, close date
  - Extract from `.quizattemptsummary`: state, grade, submission date
  - Detect "Attempt quiz now" button vs "No more attempts"

New structs:

```go
type AssignmentDetail struct {
    SubmissionStatus string  // "Submitted for grading" / "No submission"
    GradingStatus    string  // "Not graded" / "Graded"
    DueDate          string
    TimeRemaining    string
    LastModified     string
    FileSubmissions  []string // file names
    HasEditButton    bool
}

type QuizDetail struct {
    AttemptsAllowed  string  // "1" / "Unlimited"
    CloseDate        string
    AttemptState     string  // "Finished" / ""
    AttemptGrade     string  // "100.00" / ""
    AttemptDate      string
    CanAttempt       bool    // true if "Attempt quiz now" button exists
    NoMoreAttempts   bool    // true if "No more attempts" text exists
}
```

#### 1D. Add detail page client methods (`internal/moodle/client.go`)

```go
func (c *Client) GetAssignmentDetail(ctx, assignmentID) (AssignmentDetail, error)
func (c *Client) GetQuizDetail(ctx, quizID) (QuizDetail, error)
```

URL patterns:

- Assignment: `https://elearning.budiluhur.ac.id/mod/assign/view.php?id={ITEM_ID}`
- Quiz: `https://elearning.budiluhur.ac.id/mod/quiz/view.php?id={ITEM_ID}`

These need new base URLs in config or derive from existing URLs.

#### 1E. Evolve SQLite schema (`internal/notify/store.go`)

Add columns to `notification_events` via migration:

```sql
ALTER TABLE notification_events ADD COLUMN due_date TEXT NOT NULL DEFAULT '';
ALTER TABLE notification_events ADD COLUMN submission_status TEXT NOT NULL DEFAULT '';
ALTER TABLE notification_events ADD COLUMN grade TEXT NOT NULL DEFAULT '';
ALTER TABLE notification_events ADD COLUMN due_date_parsed TEXT;  -- RFC3339 for querying
ALTER TABLE notification_events ADD COLUMN reminder_24h_sent INTEGER NOT NULL DEFAULT 0;
ALTER TABLE notification_events ADD COLUMN reminder_12h_sent INTEGER NOT NULL DEFAULT 0;
ALTER TABLE notification_events ADD COLUMN detail_fetched INTEGER NOT NULL DEFAULT 0;
ALTER TABLE notification_events ADD COLUMN suggestion_status TEXT NOT NULL DEFAULT '';
ALTER TABLE notification_events ADD COLUMN suggestion_text TEXT NOT NULL DEFAULT '';
```

Add schema migration versioning (simple: check if column exists before ALTER).

Update `NotificationEvent` struct and `UpsertEvents()` to include new fields.
Update `UpsertEvents` ON CONFLICT to also update `due_date`, `submission_status`, `grade`, `due_date_parsed`.

#### 1F. Update runner event creation (`internal/runner/runner.go`)

Update `fetchAssignmentsAndQuizzes()` to populate new fields in `NotificationEvent`:

- `DueDate` / `CloseDate` → `due_date` column
- `SubmissionStatus` → `submission_status` column
- `Grade` → `grade` column
- Parse date string → `due_date_parsed` (RFC3339)

---

### Phase 2: Detail Page Fetching

**Goal:** After list-page scan, fetch detail pages for items that need richer data.

#### 2A. Add detail fetch logic in runner

After the list-page upsert, query items where `detail_fetched = 0` and fetch their detail pages.
Update the SQLite record with detail info. Mark `detail_fetched = 1`.

Rate-limit detail page fetches to avoid hammering Moodle (reuse existing `rate.Limiter`).

#### 2B. Add config for detail fetching

Add to config:

```go
DetailFetchEnabled bool `env:"DETAIL_FETCH_ENABLED"` // default true
DetailFetchLimit   int  `env:"DETAIL_FETCH_LIMIT"`   // max detail pages per run, default 10
```

#### 2C. Store detail data

Add new table or extend existing:

```sql
CREATE TABLE IF NOT EXISTS item_details (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id INTEGER NOT NULL REFERENCES notification_events(id),
    event_type TEXT NOT NULL,
    item_id TEXT NOT NULL,
    submission_status TEXT NOT NULL DEFAULT '',
    grading_status TEXT NOT NULL DEFAULT '',
    due_date TEXT NOT NULL DEFAULT '',
    attempts_allowed TEXT NOT NULL DEFAULT '',
    attempt_state TEXT NOT NULL DEFAULT '',
    attempt_grade TEXT NOT NULL DEFAULT '',
    can_attempt INTEGER NOT NULL DEFAULT 0,
    no_more_attempts INTEGER NOT NULL DEFAULT 0,
    file_submissions TEXT NOT NULL DEFAULT '',  -- JSON array
    raw_content TEXT NOT NULL DEFAULT '',  -- full text for LLM
    fetched_at TEXT NOT NULL,
    UNIQUE(event_type, item_id)
);
```

---

### Phase 3: Deadline Reminder System

**Goal:** Send WhatsApp reminders at 24h and 12h before due date for unsubmitted items.

#### 3A. Add reminder check function (`internal/runner/reminder.go`)

New file with:

```go
func (r *Runner) checkDeadlineReminders(ctx context.Context, now time.Time) error
```

Logic:

1. Query `notification_events` WHERE `due_date_parsed IS NOT NULL AND submission_status != 'Submitted for grading'`
2. For each item:
   - If due in ≤24h AND `reminder_24h_sent = 0` → send 24h reminder, set flag
   - If due in ≤12h AND `reminder_12h_sent = 0` → send 12h reminder, set flag
3. Skip items already past due

#### 3B. Reminder message format

```
⏰ Reminder: Assignment deadline approaching!

Mata Kuliah: {CourseName}
Item: {ItemName}
Due: {DueDate}
Time Left: ~{hours}h {minutes}m
Status: {SubmissionStatus}
Link: {ItemLink}
```

#### 3C. Wire into runner

Call `checkDeadlineReminders()` after `fetchAssignmentsAndQuizzes()` in `RunAttendance()`.

#### 3D. Add store methods

```go
func (s *NotificationStore) ListApproachingDeadlines(ctx, now, window time.Duration) ([]PendingNotification, error)
func (s *NotificationStore) MarkReminder24hSent(ctx, id int64) error
func (s *NotificationStore) MarkReminder12hSent(ctx, id int64) error
```

---

### Phase 4: LLM Suggestion System

**Goal:** Send quiz/assignment content to OpenRouter API, get AI suggestions, notify via WhatsApp.

#### 4A. Add LLM client (`internal/llm/client.go`)

New package `internal/llm/`:

```go
type Client struct {
    Endpoint string  // https://openrouter.ai/api/v1/chat/completions
    APIKey   string
    Model    string  // e.g. "anthropic/claude-sonnet-4-20250514"
    HC       *http.Client
}

type SuggestionRequest struct {
    EventType  string  // "quiz" or "assignment"
    CourseName string
    ItemName   string
    ItemTitle  string
    Content    string  // extracted from detail page
}

type SuggestionResponse struct {
    Suggestion string
    Model      string
    TokensUsed int
}

func (c *Client) GetSuggestion(ctx, req SuggestionRequest) (SuggestionResponse, error)
```

OpenRouter API format:

```json
POST https://openrouter.ai/api/v1/chat/completions
Authorization: Bearer {OPENROUTER_API_KEY}
{
  "model": "{model}",
  "messages": [
    {"role": "system", "content": "You are an academic assistant..."},
    {"role": "user", "content": "Help me with this {type}: {content}"}
  ]
}
```

#### 4B. Add config fields

```go
OpenRouterEndpoint string `env:"OPENROUTER_ENDPOINT"` // default https://openrouter.ai/api/v1/chat/completions
OpenRouterAPIKey   string `env:"OPENROUTER_API_KEY"`
OpenRouterModel    string `env:"OPENROUTER_MODEL"`     // default "anthropic/claude-sonnet-4-20250514"
SuggestionEnabled  bool   `env:"SUGGESTION_ENABLED"`   // default false
```

#### 4C. Add suggestion pipeline in runner (`internal/runner/suggestion.go`)

New file:

```go
func (r *Runner) processSuggestions(ctx context.Context) error
```

Logic:

1. Query items WHERE `suggestion_status = '' OR suggestion_status = 'pending'` AND `detail_fetched = 1`
2. For unsubmitted items only (don't waste tokens on completed work)
3. Send content to OpenRouter
4. Store suggestion in `suggestion_text`, set `suggestion_status = 'ready'`
5. Send WhatsApp notification with suggestion

#### 4D. Suggestion message format

```
💡 AI Suggestion Ready!

Mata Kuliah: {CourseName}
Item: {ItemName}
Due: {DueDate}

Suggestion:
{suggestion_text}

Link: {ItemLink}
```

#### 4E. Store methods

```go
func (s *NotificationStore) ListNeedingSuggestion(ctx, limit int) ([]PendingNotification, error)
func (s *NotificationStore) UpdateSuggestion(ctx, id int64, status, text string) error
```

---

### Phase 5: Enhanced Notification Messages

**Goal:** Make notifications richer with deadline and status info.

#### 5A. Update "new item" notification format

Current:

```
📚 Assignment baru terdeteksi!
Mata Kuliah: X
Topik: Y
Item: Z
Link: L
```

Enhanced:

```
📚 Assignment baru terdeteksi!

Mata Kuliah: {CourseName}
Topik: {Title}
Item: {ItemName}
Due: {DueDate}
Status: {SubmissionStatus}
Grade: {Grade}
Link: {ItemLink}
```

#### 5B. Update `buildNotificationMessage()` in `runner.go`

Include due date and submission status if available.

---

### Phase 6: Tests

#### 6A. Parser tests (`internal/moodle/parser_test.go`)

- Test `parseAssignmentList()` with sample HTML from PLAN doc — verify due date, status, grade extraction
- Test `parseQuizList()` with sample HTML — verify close date, grade extraction
- Test `parseAssignmentDetail()` with sample HTML — verify all fields
- Test `parseQuizDetail()` with sample HTML — verify all fields
- Test `parseMoodleDate()` with various date formats
- Test edge cases: empty tables, missing columns, partial data

#### 6B. Reminder tests (`internal/runner/reminder_test.go`)

- Test 24h reminder triggers at correct time
- Test 12h reminder triggers at correct time
- Test no reminder for submitted items
- Test no reminder for past-due items
- Test reminder not re-sent (flag check)

#### 6C. Store migration tests (`internal/notify/store_test.go`)

- Test schema migration adds new columns
- Test upsert with new fields
- Test `ListApproachingDeadlines()` query
- Test reminder flag updates

#### 6D. LLM client tests (`internal/llm/client_test.go`)

- Test request building
- Test response parsing
- Test error handling (API errors, rate limits, timeouts)

---

### Phase 7: Config & Documentation

#### 7A. Update `.env.example`

Add all new env vars with clear comments.

#### 7B. Update README.md

Document:

- New quiz/assignment tracking features
- Deadline reminder system
- LLM suggestion system
- New env vars and their defaults
- Example configurations

---

## File Change Summary

| File                               | Action        | Description                                                                                       |
| ---------------------------------- | ------------- | ------------------------------------------------------------------------------------------------- |
| `internal/moodle/client.go`        | Modify        | Extend Assignment/Quiz structs, add detail page methods, add detail base URLs                     |
| `internal/moodle/parser.go`        | Modify        | Parse due dates, status, grades from list pages; add detail page parsers; add `parseMoodleDate()` |
| `internal/moodle/parser_test.go`   | Create        | Parser tests with sample HTML                                                                     |
| `internal/notify/store.go`         | Modify        | Schema migration for new columns, new query methods                                               |
| `internal/notify/store_test.go`    | Create/Modify | Tests for new store methods                                                                       |
| `internal/runner/runner.go`        | Modify        | Wire new fields into events, call reminders and suggestions                                       |
| `internal/runner/reminder.go`      | Create        | Deadline reminder logic                                                                           |
| `internal/runner/reminder_test.go` | Create        | Reminder tests                                                                                    |
| `internal/runner/suggestion.go`    | Create        | LLM suggestion pipeline                                                                           |
| `internal/llm/client.go`           | Create        | OpenRouter LLM client                                                                             |
| `internal/llm/client_test.go`      | Create        | LLM client tests                                                                                  |
| `internal/config/config.go`        | Modify        | New env vars for detail fetch, LLM, suggestions                                                   |
| `cmd/gostudentubl/main.go`         | Modify        | Initialize LLM client, pass to runner                                                             |
| `.env.example`                     | Modify        | Document new env vars                                                                             |
| `README.md`                        | Modify        | Document new features                                                                             |

## Dependency Additions

None required. All needed packages are already in `go.mod`:

- `goquery` for HTML parsing
- `modernc.org/sqlite` for database
- `net/http` for OpenRouter API (standard lib)
- `encoding/json` for API payloads (standard lib)

## Execution Order

1. **Phase 1** (1A→1F): Enhanced parsing + schema — foundation for everything else
2. **Phase 2** (2A→2C): Detail page fetching — needed for LLM content
3. **Phase 3** (3A→3D): Deadline reminders — high user value
4. **Phase 4** (4A→4E): LLM suggestions — requires detail content
5. **Phase 5** (5A→5B): Enhanced messages — polish
6. **Phase 6** (6A→6D): Tests — validate all new code
7. **Phase 7** (7A→7B): Documentation — final touch

## Risk & Notes

- **Rate limiting**: Detail page fetches add more HTTP requests per run. Use `DETAIL_FETCH_LIMIT` to cap.
- **Schema migration**: ALTER TABLE on SQLite with existing data. Use IF NOT EXISTS / check column existence.
- **Date parsing**: Moodle date format may vary by locale. Handle gracefully with fallback.
- **OpenRouter costs**: LLM calls cost money. Default `SUGGESTION_ENABLED=false`. Only process unsubmitted items.
- **Detail page auth**: Detail pages require active Moodle session. Fetch during the same login session in runner.
- **Quiz detail URL**: Need base URL for quiz detail. Derive from existing quiz list URL (`/mod/quiz/index.php` → `/mod/quiz/view.php`) or use the link already extracted from the list page (it's a full URL in the `href`).
