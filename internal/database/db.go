package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB wraps the database connection and provides initialization
type DB struct {
	*sql.DB
}

// NewDB creates and initializes a new database connection
func NewDB(dbPath string) (*DB, error) {
	// Ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open SQLite database with performance optimizations
	sqlDB, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=cache_size(10000)&_pragma=temp_store(MEMORY)&_pragma=busy_timeout(30000)")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool for better concurrency
	sqlDB.SetMaxOpenConns(1)  // SQLite works better with single connection for WAL mode
	sqlDB.SetMaxIdleConns(1)
	
	// Test the connection
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db := &DB{DB: sqlDB}

	// Initialize schema
	if err := db.initSchema(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return db, nil
}

// initSchema creates the database tables and indexes
func (db *DB) initSchema() error {
	schema := `
-- Proxy storage and caching schema
CREATE TABLE IF NOT EXISTS proxies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    host TEXT NOT NULL,
    port INTEGER NOT NULL,
    proxy_type TEXT NOT NULL,
    country TEXT,
    anonymity TEXT,
    https BOOLEAN DEFAULT 0,
    
    -- Health tracking
    status TEXT NOT NULL DEFAULT 'unknown', -- healthy, unhealthy, timeout, error, unknown
    response_time_ms INTEGER,
    fail_count INTEGER DEFAULT 0,
    
    -- Timestamps
    first_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_checked_at DATETIME,
    last_healthy_at DATETIME,
    
    -- Create unique constraint on host:port combination
    UNIQUE(host, port)
);

-- Index for fast lookups by host:port
CREATE INDEX IF NOT EXISTS idx_proxies_host_port ON proxies(host, port);

-- Index for finding proxies that need checking (by last_checked_at)
CREATE INDEX IF NOT EXISTS idx_proxies_last_checked ON proxies(last_checked_at);

-- Index for finding healthy proxies
CREATE INDEX IF NOT EXISTS idx_proxies_status ON proxies(status);

-- Index for finding proxies by type
CREATE INDEX IF NOT EXISTS idx_proxies_type ON proxies(proxy_type);`

	_, err := db.Exec(schema)
	return err
}
