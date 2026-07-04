package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/lovelytoaster94/routerhub/internal/config"
	"github.com/lovelytoaster94/routerhub/internal/server"
	"github.com/lovelytoaster94/routerhub/internal/storage"
)

// SQLite database file, resolved relative to the process working directory.
// In Docker the WORKDIR is /data (mounted volume), so the file lives at
// /data/routerhub.db. Locally the file is created in the repo root.
const dbPath = "routerhub.db"

func main() {
	// Config: config.yaml (optional) → ROUTERHUB_HOST / ROUTERHUB_PORT env override.
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize database
	db, err := storage.InitDB(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Run migrations
	if err := storage.Migrate(db); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Build server
	srv := server.New(db, cfg)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("RouterHub starting on %s (db=%s)", addr, dbPath)
	if err := http.ListenAndServe(addr, srv); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
