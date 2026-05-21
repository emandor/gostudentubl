package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const cookieName = "draft_session"

type server struct {
	cfg config
	db  *sql.DB
	mux *http.ServeMux
}

func newServer(cfg config, db *sql.DB) *server {
	s := &server{cfg: cfg, db: db}
	s.initAuditTable()
	s.initOperationalTables()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleSecurityForm)
	mux.HandleFunc("POST /auth", s.handleAuth)
	mux.HandleFunc("GET /dashboard", s.requireAuth(s.handleDashboard))
	mux.HandleFunc("GET /drafts", s.requireAuth(s.handleListDrafts))
	mux.HandleFunc("GET /drafts/{id}", s.requireAuth(s.handleViewDraft))
	mux.HandleFunc("GET /artifacts/{id}/{kind}", s.requireAuth(s.handleArtifact))
	mux.HandleFunc("GET /open", s.handleListOpen)
	mux.HandleFunc("GET /open/{id}", s.handleViewOpen)
	mux.HandleFunc("POST /admin/open/{id}", s.requireAuth(s.handleMarkOpen))
	mux.HandleFunc("POST /admin/approve-pdf/{id}", s.requireAuth(s.handleApprovePDF))
	mux.HandleFunc("GET /runs", s.requireAuth(s.handleRuns))
	mux.HandleFunc("GET /audit", s.requireAuth(s.handleAudit))
	s.mux = mux
	return s
}

// statusCapture wraps ResponseWriter to capture the HTTP status code.
type statusCapture struct {
	http.ResponseWriter
	status int
}

func (sc *statusCapture) WriteHeader(code int) {
	if sc.status == 0 {
		sc.status = code
	}
	sc.ResponseWriter.WriteHeader(code)
}

func (sc *statusCapture) statusCode() int {
	if sc.status == 0 {
		return 200
	}
	return sc.status
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sc := &statusCapture{ResponseWriter: w}
	s.mux.ServeHTTP(sc, r)
	go s.recordAccess(r, sc.statusCode())
}

// ─── Auth ────────────────────────────────────────────────────────────────────

func (s *server) signCookie(ts int64) string {
	payload := fmt.Sprintf("authenticated:%d", ts)
	mac := hmac.New(sha256.New, s.cfg.SessionSecret)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s:%s", payload, sig)
}

func (s *server) verifyCookie(value string) bool {
	// Format: "authenticated:{ts}:{hmac}"
	parts := strings.SplitN(value, ":", 3)
	if len(parts) != 3 || parts[0] != "authenticated" {
		return false
	}
	ts, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return false
	}
	if time.Since(time.Unix(ts, 0)) > 24*time.Hour {
		return false
	}
	payload := fmt.Sprintf("authenticated:%d", ts)
	mac := hmac.New(sha256.New, s.cfg.SessionSecret)
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(parts[2]), []byte(expected)) == 1
}

func (s *server) isAuthenticated(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	return s.verifyCookie(c.Value)
}

func (s *server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.isAuthenticated(r) {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		next(w, r)
	}
}

// ─── Audit / Access Logging ───────────────────────────────────────────────────

func (s *server) initAuditTable() {
	s.db.Exec(`CREATE TABLE IF NOT EXISTS access_logs (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		ts         TEXT    NOT NULL,
		ip         TEXT    NOT NULL,
		method     TEXT    NOT NULL,
		path       TEXT    NOT NULL,
		status     INTEGER NOT NULL,
		user_agent TEXT    NOT NULL DEFAULT '',
		suspicious INTEGER NOT NULL DEFAULT 0,
		note       TEXT    NOT NULL DEFAULT ''
	)`)
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_access_logs_ts ON access_logs(ts DESC)`)
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_access_logs_suspicious ON access_logs(suspicious) WHERE suspicious>0`)
}

func (s *server) initOperationalTables() {
	s.db.Exec(`CREATE TABLE IF NOT EXISTS automation_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		job_name TEXT NOT NULL,
		owner TEXT NOT NULL DEFAULT '',
		started_at TEXT NOT NULL,
		finished_at TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL,
		total_courses INTEGER NOT NULL DEFAULT 0,
		matched_courses INTEGER NOT NULL DEFAULT 0,
		attendance_found INTEGER NOT NULL DEFAULT 0,
		attendance_submitted INTEGER NOT NULL DEFAULT 0,
		assignments_found INTEGER NOT NULL DEFAULT 0,
		quizzes_found INTEGER NOT NULL DEFAULT 0,
		notifications_sent INTEGER NOT NULL DEFAULT 0,
		reminders_sent INTEGER NOT NULL DEFAULT 0,
		drafts_ready INTEGER NOT NULL DEFAULT 0,
		error TEXT NOT NULL DEFAULT ''
	)`)
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_automation_runs_started_at ON automation_runs(started_at DESC)`)
	s.db.Exec(`CREATE TABLE IF NOT EXISTS draft_reviews (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_id INTEGER NOT NULL UNIQUE,
		status TEXT NOT NULL DEFAULT '',
		score INTEGER NOT NULL DEFAULT 0,
		notes TEXT NOT NULL DEFAULT '',
		reviewed_text TEXT NOT NULL DEFAULT '',
		model TEXT NOT NULL DEFAULT '',
		tokens_used INTEGER NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL DEFAULT ''
	)`)
	s.db.Exec(`CREATE TABLE IF NOT EXISTS submission_artifacts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_id INTEGER NOT NULL UNIQUE,
		status TEXT NOT NULL DEFAULT '',
		pdf_path TEXT NOT NULL DEFAULT '',
		html_path TEXT NOT NULL DEFAULT '',
		approved INTEGER NOT NULL DEFAULT 0,
		approved_at TEXT NOT NULL DEFAULT '',
		generated_at TEXT NOT NULL DEFAULT '',
		error TEXT NOT NULL DEFAULT ''
	)`)
}

// suspiciousPatterns are path substrings that indicate scanning / attack attempts.
var suspiciousPatterns = []string{
	".env", "wp-login", "wp-admin", "phpMyAdmin", "phpmyadmin",
	"admin", "/.git", "passwd", "/etc/", "eval(", "../", "..%2f",
	"shell", "cmd.exe", "powershell", "sqlmap", "xmlrpc",
	"actuator", "config.php", "setup.php", "install.php",
	"login.php", "manager/html", "solr/", "jndi:", "union+select",
	"select+from", "<script", "alert(", "onload=",
}

var suspiciousUA = []string{
	"sqlmap", "nikto", "nmap", "masscan", "zgrab", "curl/",
	"python-requests", "go-http-client", "libwww-perl",
	"dirbuster", "gobuster", "wfuzz", "nuclei",
}

func classifySuspicious(path, ua, method string) (level int, note string) {
	pathLow := strings.ToLower(path)
	uaLow := strings.ToLower(ua)
	for _, p := range suspiciousPatterns {
		if strings.Contains(pathLow, p) {
			return 2, "path-scan:" + p
		}
	}
	for _, u := range suspiciousUA {
		if strings.Contains(uaLow, u) {
			return 2, "scanner-ua:" + u
		}
	}
	if method == "CONNECT" || method == "TRACE" {
		return 1, "method:" + method
	}
	if ua == "" {
		return 1, "empty-ua"
	}
	return 0, ""
}

func realIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip, _, err := net.SplitHostPort(strings.TrimSpace(strings.Split(xff, ",")[0])); err == nil {
			return ip
		}
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	if xri := r.Header.Get("X-Real-Ip"); xri != "" {
		return xri
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

func (s *server) recordAccess(r *http.Request, status int) {
	ip := realIP(r)
	ua := r.UserAgent()
	path := r.URL.RequestURI()
	method := r.Method
	ts := time.Now().UTC().Format(time.RFC3339)
	suspicious, note := classifySuspicious(path, ua, method)
	s.db.Exec(`INSERT INTO access_logs(ts,ip,method,path,status,user_agent,suspicious,note) VALUES(?,?,?,?,?,?,?,?)`,
		ts, ip, method, path, status, ua, suspicious, note)
}

func (s *server) handleAudit(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("filter") // "all" or "suspicious"
	if filter == "" {
		filter = "all"
	}

	query := `SELECT ts, ip, method, path, status, user_agent, suspicious, note FROM access_logs ORDER BY id DESC LIMIT 200`
	if filter == "suspicious" {
		query = `SELECT ts, ip, method, path, status, user_agent, suspicious, note FROM access_logs WHERE suspicious>0 ORDER BY id DESC LIMIT 200`
	}
	rows, err := s.db.Query(query)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var logs []auditLogEntry
	for rows.Next() {
		var e auditLogEntry
		rows.Scan(&e.TS, &e.IP, &e.Method, &e.Path, &e.Status, &e.UA, &e.Suspicious, &e.Note)
		logs = append(logs, e)
	}

	// Summary stats
	var totalToday, suspCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM access_logs WHERE ts >= date('now')`).Scan(&totalToday)
	s.db.QueryRow(`SELECT COUNT(*) FROM access_logs WHERE suspicious>0`).Scan(&suspCount)

	var topIPs []auditTopIP
	ipRows, _ := s.db.Query(`SELECT ip, COUNT(*) as n FROM access_logs GROUP BY ip ORDER BY n DESC LIMIT 8`)
	if ipRows != nil {
		defer ipRows.Close()
		for ipRows.Next() {
			var t auditTopIP
			ipRows.Scan(&t.IP, &t.Count)
			topIPs = append(topIPs, t)
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, auditPage(logs, topIPs, totalToday, suspCount, filter))
}

// ─── Handlers ────────────────────────────────────────────────────────────────

func (s *server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	summary := s.queryDashboardSummary()
	drafts, _ := s.queryDrafts(`
		SELECT ne.id, ne.event_type, ne.course_name, ne.item_title, ne.item_name,
		       ne.item_link, ne.due_date,
		       ne.draft_status, ne.draft_text, ne.draft_provider, ne.draft_model,
		       COALESCE(ne.draft_updated_at,''), COALESCE(ne.due_date_parsed,''),
		       '',
		       COALESCE((SELECT GROUP_CONCAT(provider,',') FROM draft_results WHERE event_id=ne.id AND draft_text!='' ORDER BY provider),'')
		FROM notification_events ne
		WHERE ne.draft_status IN ('ready','open')
		ORDER BY ne.id DESC
		LIMIT 6`)
	runs, _ := s.queryRuns(6)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, dashboardPage(summary, drafts, runs))
}

func (s *server) handleSecurityForm(w http.ResponseWriter, r *http.Request) {
	if s.isAuthenticated(r) {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, loginPage(s.cfg.SecurityQuestion, ""))
}

func (s *server) handleAuth(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	submitted := strings.TrimSpace(r.FormValue("answer"))

	// Accept either "subandi" or "sodara" (or configured answer).
	accepted := false
	for _, valid := range []string{s.cfg.SecurityAnswer, "subandi", "sodara"} {
		if subtle.ConstantTimeCompare(
			[]byte(strings.ToLower(submitted)),
			[]byte(strings.ToLower(valid)),
		) == 1 {
			accepted = true
			break
		}
	}

	if !accepted {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, loginPage(s.cfg.SecurityQuestion, "Jawaban salah. Coba lagi."))
		return
	}

	ts := time.Now().Unix()
	val := s.signCookie(ts)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    val,
		Path:     "/",
		MaxAge:   int((24 * time.Hour).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func (s *server) handleListDrafts(w http.ResponseWriter, r *http.Request) {
	drafts, err := s.queryDrafts(`
		SELECT ne.id, ne.event_type, ne.course_name, ne.item_title, ne.item_name,
		       ne.item_link, ne.due_date,
		       ne.draft_status, ne.draft_text, ne.draft_provider, ne.draft_model,
		       COALESCE(ne.draft_updated_at,''), COALESCE(ne.due_date_parsed,''),
		       '',
		       COALESCE((SELECT GROUP_CONCAT(provider,',') FROM draft_results WHERE event_id=ne.id AND draft_text!='' ORDER BY provider),'')
		FROM notification_events ne
		WHERE ne.draft_status IN ('ready','open')
		ORDER BY ne.id DESC`)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, draftsListPage(drafts, true))
}

func (s *server) handleViewDraft(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	d, err := s.queryDraftByID(id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, draftDetailPage(d, true))
}

func (s *server) handleListOpen(w http.ResponseWriter, r *http.Request) {
	drafts, err := s.queryDrafts(`
		SELECT ne.id, ne.event_type, ne.course_name, ne.item_title, ne.item_name,
		       ne.item_link, ne.due_date,
		       ne.draft_status, ne.draft_text, ne.draft_provider, ne.draft_model,
		       COALESCE(ne.draft_updated_at,''), COALESCE(ne.due_date_parsed,''),
		       '',
		       COALESCE((SELECT GROUP_CONCAT(provider,',') FROM draft_results WHERE event_id=ne.id AND draft_text!='' ORDER BY provider),'')
		FROM notification_events ne
		WHERE ne.draft_status = 'open'
		ORDER BY ne.id DESC`)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, draftsListPage(drafts, false))
}

func (s *server) handleViewOpen(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	d, err := s.queryDraftByID(id)
	if err != nil || d.DraftStatus != "open" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, draftDetailPage(d, false))
}

func (s *server) handleMarkOpen(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(r.Context(), `
		UPDATE notification_events
		SET draft_status = 'open', draft_updated_at = ?, updated_at = ?
		WHERE id = ? AND draft_status = 'ready'`,
		now, now, id)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/drafts", http.StatusFound)
}

func (s *server) handleApprovePDF(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(r.Context(), `
		UPDATE submission_artifacts
		SET approved = 1, approved_at = ?
		WHERE event_id = ? AND status = 'ready'`,
		now, id)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/drafts/%d", id), http.StatusFound)
}

func (s *server) handleArtifact(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	kind := strings.ToLower(strings.TrimSpace(r.PathValue("kind")))
	if kind != "pdf" && kind != "html" && kind != "manifest" {
		http.Error(w, "invalid artifact kind", http.StatusBadRequest)
		return
	}

	var relPath string
	column := "pdf_path"
	if kind == "html" || kind == "manifest" {
		column = "html_path"
	}
	query := fmt.Sprintf("SELECT %s FROM submission_artifacts WHERE event_id = ? AND status IN ('ready','html_ready','legacy_manual_pdf')", column)
	if err := s.db.QueryRowContext(r.Context(), query, id).Scan(&relPath); err != nil || strings.TrimSpace(relPath) == "" {
		http.Error(w, "artifact not found", http.StatusNotFound)
		return
	}
	if kind == "manifest" {
		relPath = filepath.Join(filepath.Dir(relPath), "submission_manifest.md")
	}

	fullPath, ok := s.safeArtifactPath(relPath)
	if !ok {
		http.Error(w, "invalid artifact path", http.StatusForbidden)
		return
	}
	if _, err := os.Stat(fullPath); err != nil {
		http.Error(w, "artifact file missing", http.StatusNotFound)
		return
	}
	if kind == "pdf" {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="submission-%d.pdf"`, id))
	} else if kind == "manifest" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	http.ServeFile(w, r, fullPath)
}

func (s *server) safeArtifactPath(relPath string) (string, bool) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" || filepath.IsAbs(relPath) {
		return "", false
	}
	clean := filepath.Clean(relPath)
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", false
	}
	base, err := filepath.Abs(s.cfg.DataDir)
	if err != nil {
		return "", false
	}
	full, err := filepath.Abs(filepath.Join(base, clean))
	if err != nil {
		return "", false
	}
	if full != base && !strings.HasPrefix(full, base+string(filepath.Separator)) {
		return "", false
	}
	return full, true
}

func (s *server) handleRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.queryRuns(100)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, runsPage(runs))
}

// ─── DB helpers ──────────────────────────────────────────────────────────────

// formatWIB formats an RFC3339 timestamp as human-readable WIB (UTC+7).
func formatWIB(rfcStr string) string {
	if rfcStr == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, rfcStr)
	if err != nil {
		return rfcStr
	}
	wib := time.FixedZone("WIB", 7*3600)
	return t.In(wib).Format("02 Jan 2006, 15:04 WIB")
}

type draftResult struct {
	Provider   string
	Model      string
	DraftText  string
	TokensUsed int
	Error      string
	Attempt    int
	CreatedAt  string
}

type draftRow struct {
	ID             int64
	EventType      string
	CourseName     string
	ItemTitle      string
	ItemName       string
	ItemLink       string
	DueDate        string
	DraftStatus    string
	DraftText      string
	DraftProvider  string
	DraftModel     string
	DraftUpdatedAt string
	DueDateParsed  string
	RawContent     string
	ProvidersList  string        // comma-separated successful providers from draft_results
	Results        []draftResult // per-provider results (only loaded for detail page)
	Review         reviewRow
	Artifact       artifactRow
}

type reviewRow struct {
	Status     string
	Score      int
	Notes      string
	Reviewed   string
	Model      string
	TokensUsed int
	UpdatedAt  string
}

type artifactRow struct {
	Status      string
	PDFPath     string
	HTMLPath    string
	Approved    bool
	ApprovedAt  string
	GeneratedAt string
	Error       string
}

type dashboardSummary struct {
	ReadyDrafts          int
	OpenDrafts           int
	PendingNotifications int
	RecentRuns           int
	FailedRuns           int
	SuspiciousTotal      int
	RequestsToday        int
	LastError            string
}

type runRow struct {
	ID                  int64
	JobName             string
	Owner               string
	StartedAt           string
	FinishedAt          string
	Status              string
	TotalCourses        int
	MatchedCourses      int
	AttendanceFound     int
	AttendanceSubmitted int
	AssignmentsFound    int
	QuizzesFound        int
	NotificationsSent   int
	RemindersSent       int
	DraftsReady         int
	Error               string
}

func (s *server) queryDashboardSummary() dashboardSummary {
	var d dashboardSummary
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM notification_events WHERE draft_status = 'ready'`).Scan(&d.ReadyDrafts)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM notification_events WHERE draft_status = 'open'`).Scan(&d.OpenDrafts)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM notification_events WHERE notified = 0`).Scan(&d.PendingNotifications)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM automation_runs WHERE started_at >= strftime('%Y-%m-%dT%H:%M:%SZ','now','-24 hours')`).Scan(&d.RecentRuns)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM automation_runs WHERE status = 'failed' AND started_at >= strftime('%Y-%m-%dT%H:%M:%SZ','now','-7 days')`).Scan(&d.FailedRuns)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM access_logs WHERE suspicious > 0`).Scan(&d.SuspiciousTotal)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM access_logs WHERE ts >= date('now')`).Scan(&d.RequestsToday)
	_ = s.db.QueryRow(`SELECT error FROM automation_runs WHERE error <> '' ORDER BY id DESC LIMIT 1`).Scan(&d.LastError)
	return d
}

func (s *server) queryRuns(limit int) ([]runRow, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT id, job_name, owner, started_at, finished_at, status,
		       total_courses, matched_courses, attendance_found, attendance_submitted,
		       assignments_found, quizzes_found, notifications_sent, reminders_sent,
		       drafts_ready, error
		FROM automation_runs
		ORDER BY id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []runRow
	for rows.Next() {
		var r runRow
		if err := rows.Scan(&r.ID, &r.JobName, &r.Owner, &r.StartedAt, &r.FinishedAt, &r.Status,
			&r.TotalCourses, &r.MatchedCourses, &r.AttendanceFound, &r.AttendanceSubmitted,
			&r.AssignmentsFound, &r.QuizzesFound, &r.NotificationsSent, &r.RemindersSent,
			&r.DraftsReady, &r.Error); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *server) queryDrafts(query string, args ...any) ([]draftRow, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []draftRow
	for rows.Next() {
		var d draftRow
		if err := rows.Scan(&d.ID, &d.EventType, &d.CourseName, &d.ItemTitle, &d.ItemName,
			&d.ItemLink, &d.DueDate, &d.DraftStatus, &d.DraftText, &d.DraftProvider, &d.DraftModel,
			&d.DraftUpdatedAt, &d.DueDateParsed, &d.RawContent, &d.ProvidersList); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *server) queryDraftByID(id int64) (draftRow, error) {
	rows, err := s.queryDrafts(`
		SELECT ne.id, ne.event_type, ne.course_name, ne.item_title, ne.item_name,
		       ne.item_link, ne.due_date,
		       ne.draft_status, ne.draft_text, ne.draft_provider, ne.draft_model,
		       COALESCE(ne.draft_updated_at,''), COALESCE(ne.due_date_parsed,''),
		       COALESCE(idt.raw_content,''),
		       COALESCE((SELECT GROUP_CONCAT(provider,',') FROM draft_results WHERE event_id=ne.id AND draft_text!='' ORDER BY provider),'')
		FROM notification_events ne
		LEFT JOIN item_details idt ON idt.event_id = ne.id
		WHERE ne.id = ?`, id)
	if err != nil {
		return draftRow{}, err
	}
	if len(rows) == 0 {
		return draftRow{}, sql.ErrNoRows
	}
	d := rows[0]

	// Load per-provider results from draft_results table if it exists.
	resRows, err := s.db.Query(`
		SELECT provider, model, draft_text, tokens_used, error, attempt, created_at
		FROM draft_results WHERE event_id = ? ORDER BY provider ASC`, id)
	if err == nil {
		defer resRows.Close()
		for resRows.Next() {
			var r draftResult
			if scanErr := resRows.Scan(&r.Provider, &r.Model, &r.DraftText, &r.TokensUsed, &r.Error, &r.Attempt, &r.CreatedAt); scanErr == nil {
				d.Results = append(d.Results, r)
			}
		}
	}
	// err != nil means table doesn't exist yet — ignore silently.
	_ = s.db.QueryRow(`
		SELECT status, score, notes, reviewed_text, model, tokens_used, updated_at
		FROM draft_reviews WHERE event_id = ?`, id).
		Scan(&d.Review.Status, &d.Review.Score, &d.Review.Notes, &d.Review.Reviewed, &d.Review.Model, &d.Review.TokensUsed, &d.Review.UpdatedAt)
	var approved int
	if err := s.db.QueryRow(`
		SELECT status, pdf_path, html_path, approved, approved_at, generated_at, error
		FROM submission_artifacts WHERE event_id = ?`, id).
		Scan(&d.Artifact.Status, &d.Artifact.PDFPath, &d.Artifact.HTMLPath, &approved, &d.Artifact.ApprovedAt, &d.Artifact.GeneratedAt, &d.Artifact.Error); err == nil {
		d.Artifact.Approved = approved == 1
	}

	return d, nil
}

// ─── HTML templates ───────────────────────────────────────────────────────────

// auditLogEntry is a single row from the access_logs table.
type auditLogEntry struct {
	TS, IP, Method, Path, UA, Note string
	Status, Suspicious             int
}

// auditTopIP holds an IP and its request count.
type auditTopIP struct {
	IP    string
	Count int
}

func auditPage(entries []auditLogEntry, ips []auditTopIP, totalToday, suspCount int, filter string) string {
	var sb strings.Builder

	// Summary bar
	sb.WriteString(`<div class="metric-grid">`)
	sb.WriteString(metricCard("Requests today", strconv.Itoa(totalToday), "HTTP requests recorded today", ""))
	sb.WriteString(metricCard("Suspicious", strconv.Itoa(suspCount), "Total suspicious requests", "danger"))
	sb.WriteString(`</div>`)

	// Top IPs
	if len(ips) > 0 {
		sb.WriteString(`<section class="panel"><h2>Top IPs</h2><div class="chip-row">`)
		for _, ip := range ips {
			fmt.Fprintf(&sb, `<span class="chip">%s <b>×%d</b></span>`, escHTML(ip.IP), ip.Count)
		}
		sb.WriteString(`</div></section>`)
	}

	// Filter buttons
	allActive := ""
	suspActive := ""
	if filter == "suspicious" {
		suspActive = " active"
	} else {
		allActive = " active"
	}
	fmt.Fprintf(&sb,
		`<div class="toolbar"><a href="/audit?filter=all" class="btn btn-ghost%s">All</a><a href="/audit?filter=suspicious" class="btn btn-ghost%s">Suspicious only (%d)</a></div>`,
		allActive, suspActive, suspCount)

	// Log table
	sb.WriteString(`<div class="table-wrap"><table class="data-table"><thead><tr>
		<th>Time</th><th>IP</th><th>Method</th><th>Path</th><th>Status</th><th>UA</th><th>Note</th>
	</tr></thead><tbody>`)

	if len(entries) == 0 {
		sb.WriteString(`<tr><td colspan="7" class="empty-cell">No records</td></tr>`)
	}
	for _, e := range entries {
		rowClass := ""
		if e.Suspicious >= 2 {
			rowClass = ` class="audit-row-red"`
		} else if e.Suspicious == 1 {
			rowClass = ` class="audit-row-yellow"`
		}
		// Status badge color
		statusClass := "status-open"
		if e.Status >= 400 && e.Status < 500 {
			statusClass = "status-queued"
		} else if e.Status >= 500 {
			statusClass = "status-generating"
		}
		// Truncate UA
		ua := e.UA
		if len(ua) > 60 {
			ua = ua[:57] + "…"
		}
		fmt.Fprintf(&sb,
			`<tr%s><td data-label="Time" class="audit-ts">%s</td><td data-label="IP" class="audit-ip">%s</td><td data-label="Method"><code>%s</code></td><td data-label="Path" class="audit-path">%s</td><td data-label="Status"><span class="status-badge %s">%d</span></td><td data-label="UA" class="audit-ua" title="%s">%s</td><td data-label="Note" class="audit-note">%s</td></tr>`,
			rowClass,
			escHTML(e.TS), escHTML(e.IP), escHTML(e.Method), escHTML(e.Path),
			statusClass, e.Status,
			escHTML(e.UA), escHTML(ua),
			escHTML(e.Note))
	}
	sb.WriteString(`</tbody></table></div>`)

	return pageWrap("Audit Log", appShell("audit", "Audit Log", "Operational security", sb.String(), true))
}

func dashboardPage(summary dashboardSummary, drafts []draftRow, runs []runRow) string {
	var sb strings.Builder
	sb.WriteString(`<div class="metric-grid">`)
	sb.WriteString(metricCard("Ready drafts", strconv.Itoa(summary.ReadyDrafts), "Awaiting publication", ""))
	sb.WriteString(metricCard("Open drafts", strconv.Itoa(summary.OpenDrafts), "Publicly visible", "success"))
	sb.WriteString(metricCard("Recent runs", strconv.Itoa(summary.RecentRuns), "Last 24 hours", ""))
	sb.WriteString(metricCard("Pending notifications", strconv.Itoa(summary.PendingNotifications), "Not yet sent", "warning"))
	sb.WriteString(metricCard("Failed runs", strconv.Itoa(summary.FailedRuns), "Last 7 days", "danger"))
	sb.WriteString(metricCard("Suspicious requests", strconv.Itoa(summary.SuspiciousTotal), "Audit log total", "danger"))
	sb.WriteString(`</div>`)

	if strings.TrimSpace(summary.LastError) != "" {
		fmt.Fprintf(&sb, `<section class="panel panel-danger"><h2>Latest failure</h2><p class="muted">%s</p></section>`, escHTML(summary.LastError))
	}

	sb.WriteString(`<div class="split-grid">`)
	sb.WriteString(`<section class="panel"><div class="panel-head"><h2>Recent drafts</h2><a href="/drafts">View all</a></div>`)
	if len(drafts) == 0 {
		sb.WriteString(`<p class="muted">No ready or open drafts yet.</p>`)
	} else {
		sb.WriteString(`<div class="compact-list">`)
		for _, d := range drafts {
			due := formatWIB(d.DueDateParsed)
			if due == "" {
				due = d.DueDate
			}
			fmt.Fprintf(&sb, `<a class="compact-row" href="/drafts/%d"><span><strong>%s</strong><small>%s</small></span><span class="status-badge status-%s">%s</span></a>`,
				d.ID, escHTML(d.ItemName), escHTML(due), d.DraftStatus, d.DraftStatus)
		}
		sb.WriteString(`</div>`)
	}
	sb.WriteString(`</section>`)

	sb.WriteString(`<section class="panel"><div class="panel-head"><h2>Recent runs</h2><a href="/runs">View all</a></div>`)
	if len(runs) == 0 {
		sb.WriteString(`<p class="muted">No automation runs recorded yet.</p>`)
	} else {
		sb.WriteString(`<div class="compact-list">`)
		for _, r := range runs {
			fmt.Fprintf(&sb, `<a class="compact-row" href="/runs"><span><strong>%s</strong><small>%s • %d attendance</small></span><span class="status-badge status-%s">%s</span></a>`,
				escHTML(r.JobName), escHTML(formatMaybeWIB(r.StartedAt)), r.AttendanceSubmitted, statusClass(r.Status), escHTML(r.Status))
		}
		sb.WriteString(`</div>`)
	}
	sb.WriteString(`</section></div>`)

	return pageWrap("Dashboard", appShell("dashboard", "Dashboard", "Moodle automation command center", sb.String(), true))
}

func runsPage(runs []runRow) string {
	var sb strings.Builder
	if len(runs) == 0 {
		sb.WriteString(`<div class="empty-state"><p>No automation runs recorded yet.</p></div>`)
	} else {
		sb.WriteString(`<div class="table-wrap"><table class="data-table"><thead><tr>
<th>#</th><th>Status</th><th>Started</th><th>Finished</th><th>Courses</th><th>Attendance</th><th>Signals</th><th>Error</th>
</tr></thead><tbody>`)
		for _, r := range runs {
			signals := fmt.Sprintf("A:%d Q:%d N:%d R:%d D:%d", r.AssignmentsFound, r.QuizzesFound, r.NotificationsSent, r.RemindersSent, r.DraftsReady)
			errText := r.Error
			if errText == "" {
				errText = "-"
			}
			fmt.Fprintf(&sb, `<tr><td data-label="#" class="id-cell">%d</td><td data-label="Status"><span class="status-badge status-%s">%s</span></td><td data-label="Started">%s</td><td data-label="Finished">%s</td><td data-label="Courses">%d/%d</td><td data-label="Attendance">%d/%d</td><td data-label="Signals"><code>%s</code></td><td data-label="Error" class="error-cell">%s</td></tr>`,
				r.ID, statusClass(r.Status), escHTML(r.Status), escHTML(formatMaybeWIB(r.StartedAt)), escHTML(formatMaybeWIB(r.FinishedAt)),
				r.MatchedCourses, r.TotalCourses, r.AttendanceSubmitted, r.AttendanceFound, escHTML(signals), escHTML(errText))
		}
		sb.WriteString(`</tbody></table></div>`)
	}
	return pageWrap("Runs", appShell("runs", "Runs", "Recent automation history", sb.String(), true))
}

func loginPage(question, errMsg string) string {
	errHTML := ""
	if errMsg != "" {
		errHTML = fmt.Sprintf(`<p class="err">%s</p>`, errMsg)
	}
	return pageWrap("Login – Draft Portal", fmt.Sprintf(`
<main class="login-screen">
<section class="login-box">
  <div class="brand-mark">ARK</div>
  <p class="eyebrow">Moodle Automation Console</p>
  <h1>Draft Portal</h1>
  <p class="question">%s</p>
  %s
  <form method="POST" action="/auth">
    <input type="text" name="answer" placeholder="Jawaban kamu..." autofocus autocomplete="off">
    <button type="submit">Masuk</button>
  </form>
</section>
</main>`, question, errHTML))
}

func draftsListPage(drafts []draftRow, isAdmin bool) string {
	var sb strings.Builder
	title := "Draft Portal"
	active := "public"
	kicker := "Published solutions"
	if isAdmin {
		title = "Drafts"
		active = "drafts"
		kicker = "AI-generated coursework drafts"
	}

	if len(drafts) == 0 {
		sb.WriteString(`<div class="empty-state"><p>Belum ada draft tersedia.</p></div>`)
	} else {
		sb.WriteString(`<div class="table-wrap"><table class="data-table"><thead><tr>
<th>#</th><th>Mata Kuliah</th><th>Item</th><th>Due</th><th>Status</th><th>Provider</th>`)
		if isAdmin {
			sb.WriteString(`<th>Aksi</th>`)
		}
		sb.WriteString(`</tr></thead><tbody>`)
		for _, d := range drafts {
			basePath := "/open"
			if isAdmin {
				basePath = "/drafts"
			}
			openBtn := ""
			if isAdmin && d.DraftStatus == "ready" {
				openBtn = fmt.Sprintf(
					`<form method="POST" action="/admin/open/%d" style="display:inline"><button class="btn btn-primary">Publish</button></form>`,
					d.ID)
			}
			due := formatWIB(d.DueDateParsed)
			if due == "" {
				due = d.DueDate
			}
			// Build provider badges — show only successful providers from draft_results;
			// fall back to the single DraftProvider if no multi-provider data.
			providerHTML := buildProviderBadges(d.ProvidersList, d.DraftProvider)
			aclHTML := ""
			if isAdmin {
				aclHTML = fmt.Sprintf(`<td data-label="Aksi">%s</td>`, openBtn)
			}
			fmt.Fprintf(&sb,
				`<tr><td data-label="#" class="id-cell">%d</td><td data-label="Mata Kuliah" class="course-cell">%s</td><td data-label="Item"><a href="%s/%d">%s</a></td><td data-label="Due" class="due-cell">%s</td><td data-label="Status"><span class="status-badge status-%s">%s</span></td><td data-label="Provider" class="provider-cell">%s</td>%s</tr>`,
				d.ID, escHTML(d.CourseName),
				basePath, d.ID, escHTML(d.ItemName),
				escHTML(due), d.DraftStatus, d.DraftStatus,
				providerHTML, aclHTML)
		}
		sb.WriteString(`</tbody></table></div>`)
	}

	return pageWrap(title, appShell(active, title, kicker, sb.String(), isAdmin))
}

func draftDetailPage(d draftRow, isAdmin bool) string {
	backPath := "/open"
	active := "public"
	if isAdmin {
		backPath = "/drafts"
		active = "drafts"
	}
	due := formatWIB(d.DueDateParsed)
	if due == "" {
		due = d.DueDate
	}

	// Optional item link chip
	linkHTML := ""
	if d.ItemLink != "" {
		linkHTML = fmt.Sprintf(`<a class="chip chip-link" href="%s" target="_blank" rel="noopener">Buka Soal ↗</a>`, escHTML(d.ItemLink))
	}

	// Optional raw_content artifact (collapsible)
	artifactHTML := ""
	if d.RawContent != "" {
		artifactHTML = fmt.Sprintf(`
<details class="artifact">
  <summary>Konteks yang dikirim ke AI</summary>
  <pre class="artifact-pre">%s</pre>
</details>`, escHTML(d.RawContent))
	}

	reviewHTML := ""
	if d.Review.Status != "" {
		reviewed := renderMarkdown(d.Review.Reviewed)
		if reviewed == "" {
			reviewed = `<p class="muted">Belum ada final reviewed answer.</p>`
		}
		reviewHTML = fmt.Sprintf(`
<section class="panel">
  <div class="panel-head"><h2>Reviewed answer</h2><span class="status-badge status-ready">score %d</span></div>
  <p class="muted">%s • %d tokens • %s</p>
  <details class="artifact"><summary>Review notes</summary><pre class="artifact-pre">%s</pre></details>
  <div class="draft-body">%s</div>
</section>`, d.Review.Score, escHTML(d.Review.Model), d.Review.TokensUsed, escHTML(d.Review.UpdatedAt), escHTML(d.Review.Notes), reviewed)
	}

	pdfHTML := ""
	if d.Artifact.Status != "" {
		approved := "not approved"
		if d.Artifact.Approved {
			approved = "approved"
		}
		artifactLinks := ""
		if d.Artifact.PDFPath != "" {
			artifactLinks += fmt.Sprintf(`<a class="btn btn-primary" href="/artifacts/%d/pdf" target="_blank" rel="noopener">Open PDF</a>`, d.ID)
		}
		if d.Artifact.HTMLPath != "" {
			artifactLinks += fmt.Sprintf(` <a class="btn btn-ghost" href="/artifacts/%d/html" target="_blank" rel="noopener">Open HTML</a>`, d.ID)
			artifactLinks += fmt.Sprintf(` <a class="btn btn-ghost" href="/artifacts/%d/manifest" target="_blank" rel="noopener">Manifest</a>`, d.ID)
		}
		approveBtn := ""
		if isAdmin && d.Artifact.Status == "ready" && !d.Artifact.Approved {
			approveBtn = fmt.Sprintf(`<form method="POST" action="/admin/approve-pdf/%d" style="display:inline"><button class="btn btn-primary">Approve for submission</button></form>`, d.ID)
		}
		pdfHTML = fmt.Sprintf(`
<section class="panel">
  <div class="panel-head"><h2>Submission artifact</h2><span class="status-badge status-%s">%s</span></div>
  <div class="toolbar">%s</div>
  <p class="muted">PDF: <code>%s</code></p>
  <p class="muted">HTML: <code>%s</code></p>
  <p class="muted">Approval: %s %s</p>
  <p class="error-cell">%s</p>
  %s
</section>`, statusClass(d.Artifact.Status), escHTML(d.Artifact.Status), artifactLinks, escHTML(d.Artifact.PDFPath), escHTML(d.Artifact.HTMLPath), escHTML(approved), escHTML(d.Artifact.ApprovedAt), escHTML(d.Artifact.Error), approveBtn)
	}

	// Build draft body — tab UI if we have multi-provider results, otherwise plain render.
	var draftBodyHTML string
	if len(d.Results) > 0 {
		draftBodyHTML = buildTabUI(d)
	} else {
		providerIcon := providerEmoji(d.DraftProvider)
		draftBodyHTML = fmt.Sprintf(`
<div class="tab-bar single-provider">
  <button class="tab active">%s %s</button>
</div>
<div class="tab-panel active">%s</div>`, providerIcon, escHTML(d.DraftProvider), renderMarkdown(d.DraftText))
	}

	body := fmt.Sprintf(`
<div>
  <a class="back-link" href="%s">← Kembali ke daftar</a>
  <div class="detail-header">
    <h1>%s</h1>
    <div class="meta-chips">
      <span class="chip">%s</span>
      <span class="chip">%s</span>
      <span class="chip">%s</span>
      <span class="status-badge status-%s">%s</span>
      %s
    </div>
    <div class="updated-at">Diperbarui: %s</div>
  </div>
  %s
  %s
  %s
  %s
</div>`,
		backPath,
		escHTML(d.ItemName),
		escHTML(d.CourseName),
		escHTML(d.ItemTitle),
		escHTML(due),
		d.DraftStatus, d.DraftStatus,
		linkHTML,
		escHTML(d.DraftUpdatedAt),
		draftBodyHTML,
		reviewHTML,
		pdfHTML,
		artifactHTML,
	)
	return pageWrap(d.ItemName+" – Draft", appShell(active, d.ItemName, "Draft detail", body, isAdmin))
}

func providerEmoji(p string) string {
	switch p {
	case "copilot":
		return "🐙"
	case "openrouter":
		return "✨"
	case "codex":
		return "🔷"
	default:
		return "🤖"
	}
}

func metricCard(label, value, detail, tone string) string {
	cls := "metric-card"
	if tone != "" {
		cls += " metric-" + tone
	}
	return fmt.Sprintf(`<section class="%s"><span>%s</span><strong>%s</strong><small>%s</small></section>`,
		cls, escHTML(label), escHTML(value), escHTML(detail))
}

func appShell(active, title, kicker, body string, authenticated bool) string {
	nav := []struct {
		Key   string
		Href  string
		Label string
	}{
		{"dashboard", "/dashboard", "Dashboard"},
		{"drafts", "/drafts", "Drafts"},
		{"public", "/open", "Open drafts"},
		{"runs", "/runs", "Runs"},
		{"audit", "/audit", "Audit"},
	}
	var navHTML strings.Builder
	for _, item := range nav {
		if !authenticated && item.Key != "public" {
			continue
		}
		cls := "shell-nav-link"
		if item.Key == active {
			cls += " active"
		}
		fmt.Fprintf(&navHTML, `<a class="%s" href="%s">%s</a>`, cls, item.Href, item.Label)
	}
	return fmt.Sprintf(`
<div class="app-shell">
  <aside class="shell-sidebar">
    <a class="shell-brand" href="%s"><span class="brand-mark">ARK</span><span>Moodle Console</span></a>
    <nav class="shell-nav">%s</nav>
  </aside>
  <main class="shell-main">
    <header class="content-header">
      <p class="eyebrow">%s</p>
      <h1>%s</h1>
    </header>
    %s
  </main>
</div>`, shellHome(authenticated), navHTML.String(), escHTML(kicker), escHTML(title), body)
}

func shellHome(authenticated bool) string {
	if authenticated {
		return "/dashboard"
	}
	return "/open"
}

func statusClass(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "open":
		return "open"
	case "failed", "error":
		return "failed"
	case "ready":
		return "ready"
	case "running", "generating":
		return "generating"
	default:
		return "queued"
	}
}

func formatMaybeWIB(rfcStr string) string {
	if strings.TrimSpace(rfcStr) == "" {
		return "-"
	}
	out := formatWIB(rfcStr)
	if out == "" {
		return rfcStr
	}
	return out
}

// buildProviderBadges returns HTML pill badges for each successful provider.
// providersList is comma-separated (from draft_results subquery); fallback is the legacy single DraftProvider.
func buildProviderBadges(providersList, fallback string) string {
	list := providersList
	if list == "" {
		list = fallback
	}
	if list == "" {
		return `<span class="provider-badge">🤖 unknown</span>`
	}
	seen := map[string]bool{}
	var sb strings.Builder
	for _, p := range strings.Split(list, ",") {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		fmt.Fprintf(&sb, `<span class="provider-badge">%s %s</span>`, providerEmoji(p), p)
	}
	return sb.String()
}

// buildTabUI generates a tab switcher showing all provider results.
func buildTabUI(d draftRow) string {
	var tabs, panels strings.Builder
	activeSet := false

	for _, r := range d.Results {
		// Skip failed results — don't show them in detail view
		if r.Error != "" || r.DraftText == "" {
			continue
		}
		active := ""
		if !activeSet {
			active = " active"
			activeSet = true
		}
		icon := providerEmoji(r.Provider)
		tabID := "tab-" + r.Provider
		tabs.WriteString(fmt.Sprintf(
			`<button class="tab%s" data-tab="%s" onclick="switchTab(event,'%s')">%s %s</button>`,
			active, tabID, tabID, icon, escHTML(r.Provider),
		))

		modelStr := ""
		if r.Model != "" && r.Model != r.Provider {
			modelStr = fmt.Sprintf(` <span class="provider-model">%s</span>`, escHTML(r.Model))
		}
		content := fmt.Sprintf(`<div class="provider-meta">%s%s • %d tokens • %s</div>%s`,
			icon, modelStr, r.TokensUsed, escHTML(r.CreatedAt), renderMarkdown(r.DraftText))
		panels.WriteString(fmt.Sprintf(`<div id="%s" class="tab-panel%s">%s</div>`, tabID, active, content))
	}

	// Fallback: nothing succeeded, show the canonical draft text
	if !activeSet {
		content := renderMarkdown(d.DraftText)
		icon := providerEmoji(d.DraftProvider)
		return fmt.Sprintf(`<div class="tab-bar"><button class="tab active">%s %s</button></div><div class="tab-panels"><div class="tab-panel active">%s</div></div>`,
			icon, escHTML(d.DraftProvider), content)
	}

	return fmt.Sprintf(`
<div class="tab-bar">%s</div>
<div class="tab-panels">%s</div>
<script>
function switchTab(e, id) {
  document.querySelectorAll('.tab').forEach(function(t){ t.classList.remove('active'); });
  document.querySelectorAll('.tab-panel').forEach(function(p){ p.classList.remove('active'); });
  e.currentTarget.classList.add('active');
  document.getElementById(id).classList.add('active');
}
</script>`, tabs.String(), panels.String())
}

func escHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&#34;")
	return s
}

func pageWrap(title, body string) string {
	const css = `
/* IBM DESIGN.md inspired dark g100 shell */
*{box-sizing:border-box;margin:0;padding:0}
:root{
  --bg:#161616;--layer:#262626;--layer-2:#393939;--layer-3:#525252;
  --border:#393939;--border-strong:#6f6f6f;--text:#f4f4f4;--text-muted:#c6c6c6;--text-subtle:#8d8d8d;
  --primary:#0f62fe;--primary-hover:#0050e6;--primary-active:#002d9c;
  --green:#24a148;--yellow:#f1c21b;--red:#da1e28;--blue:#0f62fe;
  --mono:'IBM Plex Mono','Cascadia Code','Fira Code','Courier New',monospace;
  --sans:'IBM Plex Sans','Segoe UI',system-ui,-apple-system,sans-serif;
}
html{font-size:16px;-webkit-text-size-adjust:100%;scroll-behavior:smooth}
body{background:var(--bg);color:var(--text);font-family:var(--sans);line-height:1.5;min-height:100vh}
a{color:#78a9ff;text-decoration:none}a:hover{text-decoration:underline}img{max-width:100%;height:auto}
code{font-family:var(--mono);font-size:.85em;color:#be95ff}.muted{color:var(--text-muted);font-size:.875rem}.empty-cell{text-align:center;padding:2rem;color:var(--text-subtle)}

.app-shell{min-height:100vh;display:grid;grid-template-columns:16rem minmax(0,1fr);background:var(--bg)}
.shell-sidebar{position:sticky;top:0;height:100vh;background:#0f0f0f;border-right:1px solid var(--border);display:flex;flex-direction:column}
.shell-brand{height:3rem;display:flex;align-items:center;gap:.75rem;padding:0 1rem;color:var(--text);border-bottom:1px solid var(--border);font-size:.875rem;text-decoration:none}.shell-brand:hover{text-decoration:none}
.brand-mark{display:inline-flex;align-items:center;justify-content:center;background:var(--primary);color:#fff;height:1.5rem;min-width:2rem;padding:0 .35rem;font-weight:600;font-size:.75rem;letter-spacing:.08em}
.shell-nav{padding:.5rem 0}.shell-nav-link{display:block;color:var(--text-muted);padding:.75rem 1rem;border-left:3px solid transparent;font-size:.875rem}.shell-nav-link:hover{background:var(--layer);color:var(--text);text-decoration:none}.shell-nav-link.active{background:var(--layer);border-left-color:var(--primary);color:#fff}
.shell-main{min-width:0;padding:2rem;max-width:1440px;width:100%}.content-header{border-bottom:1px solid var(--border);padding-bottom:1.5rem;margin-bottom:1.5rem}.eyebrow{font-size:.75rem;letter-spacing:.08em;text-transform:uppercase;color:var(--text-subtle);margin-bottom:.5rem}.content-header h1{font-size:clamp(2rem,5vw,3.75rem);font-weight:300;letter-spacing:-.02em;line-height:1.1}

.metric-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:1px;background:var(--border);margin-bottom:1.5rem}.metric-card{background:var(--layer);padding:1rem;min-height:9rem;display:flex;flex-direction:column;justify-content:space-between}.metric-card span{font-size:.75rem;color:var(--text-muted)}.metric-card strong{font-size:2.25rem;font-weight:300;line-height:1}.metric-card small{color:var(--text-subtle)}.metric-success strong{color:#42be65}.metric-warning strong{color:var(--yellow)}.metric-danger strong{color:#ff8389}
.split-grid{display:grid;grid-template-columns:1fr 1fr;gap:1rem}.panel{background:var(--layer);border:1px solid var(--border);padding:1rem;margin-bottom:1rem}.panel-danger{border-left:4px solid var(--red)}.panel h2,.panel-head h2{font-size:1rem;font-weight:400}.panel-head{display:flex;align-items:center;justify-content:space-between;gap:1rem;margin-bottom:1rem}.compact-list{display:flex;flex-direction:column;border-top:1px solid var(--border)}.compact-row{display:flex;align-items:center;justify-content:space-between;gap:1rem;padding:.875rem 0;border-bottom:1px solid var(--border);color:var(--text)}.compact-row:hover{text-decoration:none;background:rgba(255,255,255,.03)}.compact-row span:first-child{min-width:0}.compact-row strong{display:block;font-size:.9rem;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.compact-row small{display:block;color:var(--text-subtle);font-size:.75rem;margin-top:.2rem}

.toolbar{display:flex;gap:.5rem;margin-bottom:1rem;flex-wrap:wrap}.btn{border:0;border-radius:0;display:inline-flex;align-items:center;justify-content:center;min-height:2.5rem;padding:.65rem 1rem;font:inherit;font-size:.875rem;cursor:pointer;text-decoration:none}.btn-primary{background:var(--primary);color:#fff}.btn-primary:hover{background:var(--primary-hover);text-decoration:none}.btn-ghost{background:transparent;color:#78a9ff}.btn-ghost:hover,.btn-ghost.active{background:var(--layer-2);text-decoration:none}.btn-open{background:var(--primary);color:#fff;border:0;border-radius:0;padding:.65rem 1rem;cursor:pointer}

.table-wrap{overflow-x:auto;border:1px solid var(--border);background:var(--layer);-webkit-overflow-scrolling:touch}table{width:100%;border-collapse:collapse;font-size:.875rem}.data-table thead,thead{background:#0f0f0f}th{padding:.75rem 1rem;text-align:left;color:var(--text-muted);font-size:.75rem;font-weight:600;text-transform:uppercase;letter-spacing:.06em;white-space:nowrap}td{padding:.75rem 1rem;border-top:1px solid var(--border);vertical-align:top}tbody tr:hover td{background:rgba(255,255,255,.03)}.id-cell{color:var(--text-subtle);font-family:var(--mono);width:4rem}.course-cell,.due-cell,.provider-cell{color:var(--text-muted)}.error-cell{max-width:22rem;color:#ffb3b8;word-break:break-word}
.status-badge{display:inline-flex;align-items:center;min-height:1.5rem;padding:.125rem .5rem;font-size:.75rem;font-weight:600;text-transform:uppercase;letter-spacing:.04em;border:1px solid var(--border-strong);color:var(--text);background:transparent}.status-ready{border-color:var(--yellow);color:var(--yellow)}.status-open{border-color:var(--green);color:#42be65}.status-generating{border-color:#78a9ff;color:#78a9ff}.status-failed{border-color:#ff8389;color:#ff8389}.status-queued{border-color:var(--border-strong);color:var(--text-muted)}
.provider-badge,.chip{display:inline-flex;align-items:center;gap:.25rem;font-size:.75rem;padding:.25rem .5rem;border:1px solid var(--border);background:var(--layer-2);color:var(--text-muted);margin:1px 2px}.chip-row{display:flex;flex-wrap:wrap;gap:.35rem}.chip-link{color:#78a9ff;border-color:#78a9ff}.meta-chips{display:flex;flex-wrap:wrap;gap:.4rem;margin:.75rem 0}.updated-at{font-size:.75rem;color:var(--text-subtle)}
.empty-state{background:var(--layer);border:1px solid var(--border);padding:3rem 1rem;text-align:center;color:var(--text-muted)}

.login-screen{min-height:100vh;display:grid;place-items:center;padding:1rem;background:linear-gradient(135deg,#0f0f0f,#161616)}.login-box{width:min(100%,28rem);background:var(--layer);border:1px solid var(--border);padding:2rem}.login-box h1{font-size:2.625rem;font-weight:300;margin:.5rem 0}.question{color:var(--text-muted);margin-bottom:1.5rem}.login-box input{width:100%;height:3rem;background:#f4f4f4;color:#161616;border:0;border-bottom:2px solid var(--border-strong);padding:0 1rem;font-size:1rem;margin-bottom:1rem}.login-box input:focus{outline:2px solid var(--primary);outline-offset:-2px}.login-box button{width:100%;height:3rem;background:var(--primary);color:#fff;border:0;border-radius:0;font-size:.875rem;cursor:pointer}.err{color:#ffb3b8;background:rgba(218,30,40,.18);border-left:4px solid var(--red);padding:.75rem;margin-bottom:1rem}

.back-link{display:inline-flex;align-items:center;min-height:2.5rem;color:#78a9ff;margin-bottom:1rem}.detail-header{background:var(--layer);border:1px solid var(--border);padding:1rem;margin-bottom:1rem}.detail-header h1{font-size:clamp(1.5rem,4vw,2.625rem);font-weight:300;line-height:1.15}.tab-bar{display:flex;flex-wrap:wrap;gap:0;background:#0f0f0f;border:1px solid var(--border);border-bottom:0}.tab{background:transparent;border:0;border-right:1px solid var(--border);color:var(--text-muted);padding:.75rem 1rem;cursor:pointer}.tab.active{background:var(--layer);color:#fff;border-top:3px solid var(--primary)}.tab-panel{display:none;background:var(--layer);border:1px solid var(--border);padding:1.5rem;line-height:1.7;overflow-x:hidden}.tab-panel.active{display:block}.provider-meta{font-size:.75rem;color:var(--text-subtle);padding-bottom:.75rem;margin-bottom:1rem;border-bottom:1px solid var(--border)}.provider-model{background:var(--layer-2);color:#78a9ff;padding:.125rem .35rem;margin-left:.25rem}
.tab-panel h1,.tab-panel h2,.draft-body h1,.draft-body h2{font-weight:400;margin:1.5rem 0 .5rem}.tab-panel h1,.draft-body h1{font-size:1.5rem}.tab-panel h2,.draft-body h2{font-size:1.25rem}.tab-panel h3,.draft-body h3{font-size:1rem;margin:1rem 0 .35rem}.tab-panel p,.draft-body p{margin:.75rem 0}.tab-panel ul,.tab-panel ol,.draft-body ul,.draft-body ol{margin:.5rem 0 .5rem 1.5rem}.tab-panel blockquote,.draft-body blockquote{border-left:4px solid var(--primary);padding:.5rem 1rem;background:rgba(15,98,254,.12);color:var(--text-muted);margin:1rem 0}.tab-panel pre,.draft-body pre{background:#0f0f0f;border:1px solid var(--border);padding:1rem;overflow-x:auto}.tab-panel code,.draft-body code{font-family:var(--mono)}.table-scroll{overflow-x:auto;border:1px solid var(--border);margin:1rem 0}.tab-panel table,.draft-body table{width:max-content;min-width:100%}.artifact{margin-top:1rem;background:var(--layer);border:1px solid var(--border)}.artifact summary{padding:1rem;cursor:pointer;color:var(--text-muted)}.artifact[open] summary{border-bottom:1px solid var(--border)}.artifact-pre{padding:1rem;white-space:pre-wrap;word-break:break-word;color:var(--text-muted);font-family:var(--mono);font-size:.8rem;overflow-x:auto}
.audit-row-red{background:rgba(218,30,40,.18)}.audit-row-yellow{background:rgba(241,194,27,.12)}.audit-ts,.audit-ip,.audit-path,.audit-ua,.audit-note{font-size:.75rem}.audit-ip,.audit-path{font-family:var(--mono)}.audit-path{max-width:16rem;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.audit-ua{color:var(--text-subtle)}.audit-note{color:var(--yellow)}

@media(max-width:900px){.app-shell{grid-template-columns:1fr}.shell-sidebar{position:relative;height:auto}.shell-nav{display:flex;overflow-x:auto;padding:0}.shell-nav-link{border-left:0;border-bottom:3px solid transparent;white-space:nowrap}.shell-nav-link.active{border-bottom-color:var(--primary);border-left:0}.shell-main{padding:1rem}.metric-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.split-grid{grid-template-columns:1fr}}
@media(max-width:560px){.metric-grid{grid-template-columns:1fr}.content-header h1{font-size:2rem}.table-wrap table thead{display:none}.table-wrap table tr{display:block;border-top:1px solid var(--border);padding:.5rem 0}.table-wrap table td{display:block;border-top:0;padding:.35rem .75rem}.table-wrap table td::before{content:attr(data-label);display:block;font-size:.65rem;color:var(--text-subtle);text-transform:uppercase;letter-spacing:.06em}.id-cell{display:none}.compact-row{align-items:flex-start;flex-direction:column}}
`

	// KaTeX CDN (loaded async – no render-blocking)
	const katexHead = `
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/katex@0.16.11/dist/katex.min.css" crossorigin="anonymous">
<script defer src="https://cdn.jsdelivr.net/npm/katex@0.16.11/dist/katex.min.js" crossorigin="anonymous"></script>
<script defer src="https://cdn.jsdelivr.net/npm/katex@0.16.11/dist/contrib/auto-render.min.js" crossorigin="anonymous"></script>`

	const katexInit = `<script>
document.addEventListener("DOMContentLoaded",function(){
  if(window.renderMathInElement){
    renderMathInElement(document.body,{
      delimiters:[
        {left:"\\[",right:"\\]",display:true},
        {left:"\\(",right:"\\)",display:false}
      ],
      throwOnError:false
    });
  }
});
</script>`

	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html>\n<html lang=\"id\">\n<head>\n")
	sb.WriteString("<meta charset=\"utf-8\">\n")
	sb.WriteString("<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">\n")
	sb.WriteString("<meta name=\"theme-color\" content=\"#161616\">\n")
	sb.WriteString("<title>")
	sb.WriteString(escHTML(title))
	sb.WriteString("</title>\n")
	sb.WriteString("<link rel=\"preconnect\" href=\"https://fonts.googleapis.com\">\n")
	sb.WriteString("<link rel=\"preconnect\" href=\"https://fonts.gstatic.com\" crossorigin>\n")
	sb.WriteString("<link href=\"https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600&family=IBM+Plex+Sans:wght@300;400;500;600&display=swap\" rel=\"stylesheet\">\n")
	sb.WriteString(katexHead)
	sb.WriteString("\n<style>")
	sb.WriteString(css)
	sb.WriteString("</style>\n</head>\n<body>")
	sb.WriteString(body)
	sb.WriteString(katexInit)
	sb.WriteString("</body>\n</html>")
	return sb.String()
}
