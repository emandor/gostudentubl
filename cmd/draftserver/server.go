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
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleSecurityForm)
	mux.HandleFunc("POST /auth", s.handleAuth)
	mux.HandleFunc("GET /drafts", s.requireAuth(s.handleListDrafts))
	mux.HandleFunc("GET /drafts/{id}", s.requireAuth(s.handleViewDraft))
	mux.HandleFunc("GET /open", s.handleListOpen)
	mux.HandleFunc("GET /open/{id}", s.handleViewOpen)
	mux.HandleFunc("POST /admin/open/{id}", s.requireAuth(s.handleMarkOpen))
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

func (s *server) handleSecurityForm(w http.ResponseWriter, r *http.Request) {
	if s.isAuthenticated(r) {
		http.Redirect(w, r, "/drafts", http.StatusFound)
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
	http.Redirect(w, r, "/drafts", http.StatusFound)
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
	sb.WriteString(`<div class="audit-summary">`)
	fmt.Fprintf(&sb, `<div class="audit-stat"><span class="audit-stat-n">%d</span><span class="audit-stat-l">reqs today</span></div>`, totalToday)
	fmt.Fprintf(&sb, `<div class="audit-stat audit-suspicious"><span class="audit-stat-n">%d</span><span class="audit-stat-l">suspicious total</span></div>`, suspCount)
	sb.WriteString(`</div>`)

	// Top IPs
	if len(ips) > 0 {
		sb.WriteString(`<div class="audit-topips"><strong>Top IPs:</strong>`)
		for _, ip := range ips {
			fmt.Fprintf(&sb, `<span class="audit-ip-badge">%s <b>×%d</b></span>`, escHTML(ip.IP), ip.Count)
		}
		sb.WriteString(`</div>`)
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
		`<div class="audit-filters"><a href="/audit?filter=all" class="audit-btn%s">All</a><a href="/audit?filter=suspicious" class="audit-btn%s">⚠️ Suspicious only (%d)</a></div>`,
		allActive, suspActive, suspCount)

	// Log table
	sb.WriteString(`<div class="table-wrap"><table class="data-table"><thead><tr>
		<th>Time</th><th>IP</th><th>Method</th><th>Path</th><th>Status</th><th>UA</th><th>Note</th>
	</tr></thead><tbody>`)

	if len(entries) == 0 {
		sb.WriteString(`<tr><td colspan="7" style="text-align:center;padding:2rem;color:var(--text-muted)">No records</td></tr>`)
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
			`<tr%s><td class="audit-ts">%s</td><td class="audit-ip">%s</td><td><code>%s</code></td><td class="audit-path">%s</td><td><span class="status-badge %s">%d</span></td><td class="audit-ua" title="%s">%s</td><td class="audit-note">%s</td></tr>`,
			rowClass,
			escHTML(e.TS), escHTML(e.IP), escHTML(e.Method), escHTML(e.Path),
			statusClass, e.Status,
			escHTML(e.UA), escHTML(ua),
			escHTML(e.Note))
	}
	sb.WriteString(`</tbody></table></div>`)

	auditCSS := `
.audit-summary{display:flex;gap:1rem;margin-bottom:1.2rem;flex-wrap:wrap}
.audit-stat{background:var(--surface);border:1px solid var(--border);border-radius:var(--r);padding:.6rem 1.2rem;display:flex;flex-direction:column;align-items:center}
.audit-stat.audit-suspicious .audit-stat-n{color:var(--yellow)}
.audit-stat-n{font-size:1.6rem;font-weight:700;color:var(--accent)}
.audit-stat-l{font-size:.75rem;color:var(--text-muted)}
.audit-topips{margin-bottom:1rem;font-size:.85rem}
.audit-ip-badge{display:inline-block;margin:2px 4px;padding:2px 8px;background:var(--surface2);border:1px solid var(--border);border-radius:10px}
.audit-filters{display:flex;gap:.5rem;margin-bottom:1rem}
.audit-btn{padding:.35rem .9rem;border-radius:var(--r);background:var(--surface);border:1px solid var(--border);color:var(--text);font-size:.85rem;text-decoration:none}
.audit-btn.active{background:var(--primary);border-color:var(--primary);color:#fff}
.audit-row-red{background:rgba(248,113,113,.12)}
.audit-row-yellow{background:rgba(251,191,36,.10)}
.audit-ts{font-size:.75rem;color:var(--text-muted);white-space:nowrap}
.audit-ip{font-size:.8rem;white-space:nowrap;font-family:monospace}
.audit-path{font-size:.8rem;max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-family:monospace}
.audit-ua{font-size:.75rem;color:var(--text-muted);max-width:200px;cursor:default}
.audit-note{font-size:.75rem;color:var(--yellow)}
`

	body := fmt.Sprintf(`<div class="container"><h1 style="margin-bottom:1.2rem">🔍 Audit Log</h1>%s</div><style>%s</style>`,
		sb.String(), auditCSS)
	return pageWrap("Audit Log", body)
}

func loginPage(question, errMsg string) string {
	errHTML := ""
	if errMsg != "" {
		errHTML = fmt.Sprintf(`<p class="err">%s</p>`, errMsg)
	}
	return pageWrap("Login – Draft Portal", fmt.Sprintf(`
<div class="login-box">
  <div class="login-icon">📝</div>
  <h1>Draft Portal</h1>
  <p class="question">%s</p>
  %s
  <form method="POST" action="/auth">
    <input type="text" name="answer" placeholder="Jawaban kamu..." autofocus autocomplete="off">
    <button type="submit">Masuk →</button>
  </form>
</div>`, question, errHTML))
}

func draftsListPage(drafts []draftRow, isAdmin bool) string {
	var sb strings.Builder
	title := "Draft Portal"
	if isAdmin {
		title = "Admin — Draft Portal"
	}
	sb.WriteString(`<div class="container">`)
	sb.WriteString(`<div class="page-header"><h1>📝 `)
	sb.WriteString(escHTML(title))
	sb.WriteString(`</h1>`)
	if isAdmin {
		sb.WriteString(`<span class="badge badge-admin">Admin</span>`)
	}
	sb.WriteString(`</div>`)

	if len(drafts) == 0 {
		sb.WriteString(`<div class="empty-state"><p>📭 Belum ada draft tersedia.</p></div>`)
	} else {
		sb.WriteString(`<div class="table-wrap"><table><thead><tr>
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
					`<form method="POST" action="/admin/open/%d" style="display:inline"><button class="btn-open">Publish</button></form>`,
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
				aclHTML = fmt.Sprintf(`<td>%s</td>`, openBtn)
			}
			fmt.Fprintf(&sb,
				`<tr><td class="id-cell">%d</td><td class="course-cell">%s</td><td><a href="%s/%d">%s</a></td><td class="due-cell">%s</td><td><span class="status-badge status-%s">%s</span></td><td class="provider-cell">%s</td>%s</tr>`,
				d.ID, escHTML(d.CourseName),
				basePath, d.ID, escHTML(d.ItemName),
				escHTML(due), d.DraftStatus, d.DraftStatus,
				providerHTML, aclHTML)
		}
		sb.WriteString(`</tbody></table></div>`)
	}
	sb.WriteString(`</div>`)

	return pageWrap(title, sb.String())
}

func draftDetailPage(d draftRow, isAdmin bool) string {
	backPath := "/open"
	if isAdmin {
		backPath = "/drafts"
	}
	due := formatWIB(d.DueDateParsed)
	if due == "" {
		due = d.DueDate
	}

	// Optional item link chip
	linkHTML := ""
	if d.ItemLink != "" {
		linkHTML = fmt.Sprintf(`<a class="chip chip-link" href="%s" target="_blank" rel="noopener">🔗 Buka Soal</a>`, escHTML(d.ItemLink))
	}

	// Optional raw_content artifact (collapsible)
	artifactHTML := ""
	if d.RawContent != "" {
		artifactHTML = fmt.Sprintf(`
<details class="artifact">
  <summary>📋 Konteks yang dikirim ke AI</summary>
  <pre class="artifact-pre">%s</pre>
</details>`, escHTML(d.RawContent))
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
<div class="container">
  <a class="back-link" href="%s">← Kembali ke daftar</a>
  <div class="detail-header">
    <h1>%s</h1>
    <div class="meta-chips">
      <span class="chip">📚 %s</span>
      <span class="chip">📌 %s</span>
      <span class="chip">⏰ %s</span>
      <span class="status-badge status-%s">%s</span>
      %s
    </div>
    <div class="updated-at">Diperbarui: %s</div>
  </div>
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
		artifactHTML,
	)
	return pageWrap(d.ItemName+" – Draft", body)
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
/* ── Reset & tokens ─────────────────────────────────────────────────── */
*{box-sizing:border-box;margin:0;padding:0}
:root{
  --bg:#0f0f1a;
  --surface:#161625;
  --surface2:#1e1e32;
  --border:#2a2a45;
  --primary:#7c6af7;
  --primary-dim:#4e45b0;
  --accent:#64d2ff;
  --text:#e8e8f0;
  --text-muted:#888aaa;
  --text-dim:#555577;
  --green:#4ade80;
  --yellow:#fbbf24;
  --blue:#60a5fa;
  --red:#f87171;
  --r:8px;
  --r-lg:14px;
  --fs:16px;
}

/* ── Base ────────────────────────────────────────────────────────────── */
html{font-size:var(--fs);-webkit-text-size-adjust:100%;scroll-behavior:smooth}
body{background:var(--bg);color:var(--text);font-family:'Segoe UI',system-ui,-apple-system,sans-serif;
  line-height:1.65;min-height:100vh;padding:1.25rem 1rem}
a{color:var(--accent);text-decoration:none}
a:hover{opacity:.8;text-decoration:underline}
img{max-width:100%;height:auto}

/* ── Layout ──────────────────────────────────────────────────────────── */
.container{max-width:960px;margin:0 auto;width:100%}

/* ── Page header ─────────────────────────────────────────────────────── */
.page-header{display:flex;align-items:center;gap:.65rem;margin-bottom:1.5rem;
  padding-bottom:.85rem;border-bottom:1px solid var(--border);flex-wrap:wrap}
.page-header h1{font-size:clamp(1.15rem,4vw,1.5rem);font-weight:700;color:#fff}

/* ── Badges ──────────────────────────────────────────────────────────── */
.badge-admin{background:var(--primary-dim);color:#c8c0ff;font-size:.68rem;
  padding:.2rem .55rem;border-radius:99px;text-transform:uppercase;letter-spacing:.05em}
.status-badge{display:inline-block;padding:.2rem .6rem;border-radius:99px;
  font-size:.72rem;font-weight:600;text-transform:uppercase;letter-spacing:.04em;white-space:nowrap}
.status-ready{background:rgba(251,191,36,.15);color:var(--yellow)}
.status-open{background:rgba(74,222,128,.15);color:var(--green)}
.status-generating{background:rgba(96,165,250,.15);color:var(--blue)}

/* ── List table ──────────────────────────────────────────────────────── */
.table-wrap{overflow-x:auto;border-radius:var(--r-lg);border:1px solid var(--border);
  -webkit-overflow-scrolling:touch}
table{width:100%;border-collapse:collapse;font-size:.88rem}
thead{background:var(--surface2)}
th{padding:.7rem .9rem;text-align:left;color:var(--text-muted);font-size:.75rem;
  font-weight:600;text-transform:uppercase;letter-spacing:.06em;white-space:nowrap}
td{padding:.65rem .9rem;border-top:1px solid var(--border);vertical-align:middle}
tbody tr:active td,tbody tr:hover td{background:var(--surface2)}
.id-cell{color:var(--text-dim);font-size:.78rem;width:2.5rem}
.course-cell{color:var(--text-muted);font-size:.83rem}
.due-cell{white-space:nowrap;font-size:.8rem;color:var(--text-muted)}
.provider-cell{font-size:.8rem;color:var(--text-muted);white-space:nowrap}
.provider-badge{display:inline-block;font-size:.75rem;padding:2px 6px;border-radius:10px;background:var(--bg-card);border:1px solid var(--border);margin:1px 2px;white-space:nowrap}

/* ── Buttons ─────────────────────────────────────────────────────────── */
.btn-open{background:var(--green);color:#0a2010;border:none;
  padding:.35rem .8rem;border-radius:6px;cursor:pointer;font-size:.78rem;
  font-weight:600;min-height:32px;touch-action:manipulation}
.btn-open:active{opacity:.8}

/* ── Empty ───────────────────────────────────────────────────────────── */
.empty-state{text-align:center;padding:3rem 1rem;color:var(--text-muted)}

/* ── Login ───────────────────────────────────────────────────────────── */
.login-box{max-width:380px;margin:3rem auto;background:var(--surface);
  padding:2rem 1.5rem;border-radius:var(--r-lg);border:1px solid var(--border);text-align:center}
.login-icon{font-size:2.2rem;margin-bottom:.75rem}
.login-box h1{font-size:1.35rem;color:#fff;margin-bottom:.4rem}
.question{color:var(--text-muted);margin-bottom:1.25rem;font-size:.92rem}
.login-box input{width:100%;padding:.75rem 1rem;background:var(--surface2);
  border:1px solid var(--border);color:var(--text);border-radius:var(--r);
  font-size:1rem;margin-bottom:.85rem;outline:none;
  -webkit-appearance:none;appearance:none;transition:border-color .2s}
.login-box input:focus{border-color:var(--primary)}
.login-box button{width:100%;padding:.8rem;background:var(--primary);color:#fff;border:none;
  border-radius:var(--r);cursor:pointer;font-size:1rem;font-weight:600;
  min-height:48px;touch-action:manipulation;transition:background .15s}
.login-box button:active{background:var(--primary-dim)}
.err{color:var(--red);font-size:.85rem;margin-bottom:.85rem;
  background:rgba(248,113,113,.1);padding:.5rem .75rem;border-radius:6px}

/* ── Back link ───────────────────────────────────────────────────────── */
.back-link{display:inline-flex;align-items:center;gap:.3rem;color:var(--text-muted);
  font-size:.875rem;margin-bottom:1rem;padding:.35rem 0;min-height:44px;
  vertical-align:middle}
.back-link:hover,.back-link:active{color:var(--accent);text-decoration:none}

/* ── Detail header ───────────────────────────────────────────────────── */
.detail-header{background:var(--surface);border:1px solid var(--border);
  border-radius:var(--r-lg);padding:1.25rem;margin-bottom:1.25rem}
.detail-header h1{font-size:clamp(1rem,4vw,1.3rem);color:#fff;
  margin-bottom:.75rem;line-height:1.4;word-break:break-word}
.meta-chips{display:flex;flex-wrap:wrap;gap:.4rem;margin-bottom:.5rem}
.chip{background:var(--surface2);border:1px solid var(--border);
  color:var(--text-muted);font-size:.75rem;padding:.22rem .6rem;
  border-radius:99px;white-space:nowrap;max-width:100%;overflow:hidden;text-overflow:ellipsis}
.chip-link{color:var(--accent);border-color:var(--accent);opacity:.85}
.chip-link:hover{opacity:1;background:rgba(100,210,255,.08)}
.updated-at{font-size:.72rem;color:var(--text-dim);margin-top:.35rem}

/* ── Tab bar (multi-provider comparison) ────────────────────────────── */
.tab-bar{display:flex;flex-wrap:wrap;gap:.4rem;margin-bottom:0;
  padding:.6rem 1rem;background:var(--surface);
  border:1px solid var(--border);border-radius:var(--r-lg) var(--r-lg) 0 0}
.tab{background:transparent;border:1px solid var(--border);border-radius:var(--r);
  color:var(--text-muted);padding:.38rem .85rem;cursor:pointer;font-size:.82rem;
  transition:background .15s,color .15s;white-space:nowrap}
.tab.active{background:var(--primary);border-color:var(--primary);color:#fff;font-weight:600}
.tab:hover:not(.active){background:rgba(124,106,247,.12);color:var(--text)}
.tab-panels{border:1px solid var(--border);border-top:none;
  border-radius:0 0 var(--r-lg) var(--r-lg)}
.tab-panel{display:none;padding:1.35rem 1.5rem;line-height:1.8;overflow-x:hidden;word-break:break-word}
.tab-panel.active{display:block}
.provider-meta{font-size:.75rem;color:var(--text-dim);margin-bottom:1rem;padding-bottom:.6rem;
  border-bottom:1px solid var(--border)}
.provider-model{background:rgba(100,210,255,.1);color:var(--accent);
  border-radius:4px;padding:.1rem .35rem;margin-left:.3rem;font-size:.72rem}
.provider-error{background:rgba(255,80,80,.08);border:1px solid rgba(255,80,80,.3);
  border-radius:var(--r);padding:.9rem 1rem;color:#ff9090;font-size:.85rem;line-height:1.6}
/* Make tab-panel content inherit draft-body styles */
.tab-panel h1,.tab-panel h2,.tab-panel h3,.tab-panel h4,
.tab-panel p,.tab-panel ul,.tab-panel ol,.tab-panel li,
.tab-panel strong,.tab-panel em,.tab-panel blockquote,
.tab-panel code,.tab-panel pre,.tab-panel table,.tab-panel thead,.tab-panel th,.tab-panel td{
  /* inherit via parent selector below */
}
.tab-panels h1{font-size:clamp(1.05rem,3.5vw,1.3rem);color:#fff;
  margin:1.4rem 0 .55rem;padding-bottom:.35rem;border-bottom:1px solid var(--border)}
.tab-panels h2{font-size:clamp(1rem,3vw,1.15rem);color:#c8c8ff;margin:1.25rem 0 .45rem}
.tab-panels h3{font-size:.98rem;color:#aaaadd;margin:1.1rem 0 .35rem}
.tab-panels h4{font-size:.93rem;color:var(--text-muted);margin:.9rem 0 .25rem}
.tab-panels p{margin:.65rem 0}
.tab-panels ul,.tab-panels ol{margin:.45rem 0 .45rem 1.4rem}
.tab-panels li{margin:.22rem 0}
.tab-panels strong{color:#fff}
.tab-panels em{color:#c8d0ff}
.tab-panels blockquote{border-left:3px solid var(--primary);padding:.45rem .9rem;
  color:var(--text-muted);background:rgba(124,106,247,.07);
  border-radius:0 6px 6px 0;margin:.65rem 0}
.tab-panels code{background:#0d1420;color:#a5d8ff;padding:.1rem .3rem;
  border-radius:4px;font-family:'Cascadia Code','Fira Code','Courier New',monospace;
  font-size:.85em;word-break:break-all}
.tab-panels pre{background:#0d1420;border:1px solid var(--border);
  padding:.9rem 1rem;border-radius:var(--r);overflow-x:auto;margin:.75rem 0;
  -webkit-overflow-scrolling:touch}
.tab-panels pre code{background:transparent;padding:0;color:#cde8ff;
  font-size:.82rem;word-break:normal}
.tab-panels .table-scroll{overflow-x:auto;-webkit-overflow-scrolling:touch;
  margin:.9rem 0;border-radius:var(--r);border:1px solid var(--border)}
.tab-panels table{width:max-content;min-width:100%;border-collapse:collapse;font-size:.82rem;margin:0}
.tab-panels thead{background:rgba(124,106,247,.15)}
.tab-panels th{padding:.5rem .8rem;text-align:left;color:#c0bcff;font-size:.75rem;
  font-weight:700;text-transform:uppercase;letter-spacing:.04em}
.tab-panels td{padding:.45rem .8rem;border-top:1px solid var(--border)}

/* ── Artifact / context collapsible ─────────────────────────────────── */
.artifact{margin-top:1.5rem;border:1px solid var(--border);border-radius:var(--r);
  background:var(--surface)}
.artifact summary{padding:.65rem 1rem;cursor:pointer;font-size:.82rem;
  color:var(--text-muted);user-select:none;list-style:none;display:flex;align-items:center;gap:.4rem}
.artifact summary::-webkit-details-marker{display:none}
.artifact[open] summary{border-bottom:1px solid var(--border);color:var(--text)}
.artifact-pre{padding:1rem;font-size:.78rem;line-height:1.6;color:#aabbcc;
  font-family:'Cascadia Code','Fira Code','Courier New',monospace;
  white-space:pre-wrap;word-break:break-word;overflow-x:auto;
  -webkit-overflow-scrolling:touch}

/* ── Draft body ──────────────────────────────────────────────────────── */
.draft-body{background:var(--surface);border:1px solid var(--border);
  border-radius:var(--r-lg);padding:1.35rem 1.5rem;line-height:1.8;
  overflow-x:hidden;word-break:break-word}
.draft-body h1{font-size:clamp(1.05rem,3.5vw,1.3rem);color:#fff;
  margin:1.4rem 0 .55rem;padding-bottom:.35rem;border-bottom:1px solid var(--border)}
.draft-body h2{font-size:clamp(1rem,3vw,1.15rem);color:#c8c8ff;margin:1.25rem 0 .45rem}
.draft-body h3{font-size:.98rem;color:#aaaadd;margin:1.1rem 0 .35rem}
.draft-body h4{font-size:.93rem;color:var(--text-muted);margin:.9rem 0 .25rem}
.draft-body p{margin:.65rem 0}
.draft-body ul,.draft-body ol{margin:.45rem 0 .45rem 1.4rem}
.draft-body li{margin:.22rem 0}
.draft-body strong{color:#fff}
.draft-body em{color:#c8d0ff}
.draft-body blockquote{border-left:3px solid var(--primary);padding:.45rem .9rem;
  color:var(--text-muted);background:rgba(124,106,247,.07);
  border-radius:0 6px 6px 0;margin:.65rem 0}

/* Inline & block code */
.draft-body code{background:#0d1420;color:#a5d8ff;padding:.1rem .3rem;
  border-radius:4px;font-family:'Cascadia Code','Fira Code','Courier New',monospace;
  font-size:.85em;word-break:break-all}
.draft-body pre{background:#0d1420;border:1px solid var(--border);
  padding:.9rem 1rem;border-radius:var(--r);overflow-x:auto;margin:.75rem 0;
  -webkit-overflow-scrolling:touch}
.draft-body pre code{background:transparent;padding:0;color:#cde8ff;
  font-size:.82rem;word-break:normal}

/* Scroll containers around markdown tables */
.draft-body .table-scroll{overflow-x:auto;-webkit-overflow-scrolling:touch;
  margin:.9rem 0;border-radius:var(--r);border:1px solid var(--border)}
.draft-body table{width:max-content;min-width:100%;border-collapse:collapse;
  font-size:.82rem;margin:0}
.draft-body thead{background:rgba(124,106,247,.15)}
.draft-body th{padding:.5rem .8rem;text-align:left;color:#c0bcff;font-size:.75rem;
  font-weight:700;text-transform:uppercase;letter-spacing:.04em;
  white-space:nowrap;border-bottom:2px solid var(--primary-dim)}
.draft-body td{padding:.45rem .8rem;border-top:1px solid var(--border);
  color:var(--text);vertical-align:top}
.draft-body tbody tr:hover td,.draft-body tbody tr:active td{
  background:rgba(124,106,247,.06)}
.draft-body hr{border:none;border-top:1px solid var(--border);margin:1.35rem 0}

/* KaTeX display math centering */
.draft-body .katex-display{overflow-x:auto;overflow-y:hidden;
  padding:.5rem 0;-webkit-overflow-scrolling:touch}

/* ── Responsive ──────────────────────────────────────────────────────── */
@media(max-width:480px){
  body{padding:.85rem .65rem}
  .draft-body{padding:1rem .85rem}
  .detail-header{padding:1rem .9rem}
  .page-header h1{font-size:1.1rem}
  /* Stack table columns on very small screens */
  .table-wrap table thead{display:none}
  .table-wrap table td{
    display:block;padding:.45rem .75rem;border-top:none;border-bottom:1px solid var(--border)
  }
  .table-wrap table td::before{
    content:attr(data-label);display:block;
    font-size:.68rem;text-transform:uppercase;color:var(--text-dim);margin-bottom:.15rem
  }
  .table-wrap table tr{border-top:2px solid var(--border);display:block;margin-bottom:.25rem}
  .id-cell{display:none}
}`

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
	sb.WriteString("<meta name=\"theme-color\" content=\"#0f0f1a\">\n")
	sb.WriteString("<title>")
	sb.WriteString(escHTML(title))
	sb.WriteString("</title>\n")
	sb.WriteString(katexHead)
	sb.WriteString("\n<style>")
	sb.WriteString(css)
	sb.WriteString("</style>\n</head>\n<body>")
	sb.WriteString(body)
	sb.WriteString(katexInit)
	sb.WriteString("</body>\n</html>")
	return sb.String()
}
