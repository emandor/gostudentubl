package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"

	_ "modernc.org/sqlite"
)

type config struct {
	DBPath           string
	ServerPort       string
	SecurityQuestion string
	SecurityAnswer   string
	SessionSecret    []byte
}

func loadConfig() config {
	dbPath := os.Getenv("DRAFT_DB_PATH")
	if dbPath == "" {
		dbPath = "notifications.db"
	}
	port := os.Getenv("DRAFT_SERVER_PORT")
	if port == "" {
		port = "8082"
	}
	question := os.Getenv("DRAFT_SECURITY_QUESTION")
	if question == "" {
		question = "Siapa nama kamu?"
	}
	answer := os.Getenv("DRAFT_SECURITY_ANSWER")

	secretStr := os.Getenv("DRAFT_SESSION_SECRET")
	var secret []byte
	if secretStr == "" {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			log.Fatalf("generate session secret: %v", err)
		}
		secretStr = hex.EncodeToString(raw)
		log.Printf("DRAFT_SESSION_SECRET not set — generated: %s", secretStr)
	}
	secret = []byte(secretStr)

	return config{
		DBPath:           dbPath,
		ServerPort:       port,
		SecurityQuestion: question,
		SecurityAnswer:   answer,
		SessionSecret:    secret,
	}
}

func main() {
	cfg := loadConfig()

	// Open SQLite in read-write mode (we need write for MarkDraftOpen via store import would need write)
	// Read-only is fine for listing; for admin/open we call the store via a thin wrapper.
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000", cfg.DBPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("ping db: %v", err)
	}

	srv := newServer(cfg, db)
	addr := ":" + cfg.ServerPort
	log.Printf("draftserver listening on %s", addr)
	if err := http.ListenAndServe(addr, srv); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
