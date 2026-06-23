-- Proxy storage and caching schema. Kept in sync with db.go initSchema().
CREATE TABLE proxies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    host TEXT NOT NULL,
    port INTEGER NOT NULL,
    proxy_type TEXT NOT NULL,
    country TEXT,
    anonymity TEXT,
    https BOOLEAN DEFAULT 0,

    status TEXT NOT NULL DEFAULT 'unknown',
    response_time_ms INTEGER,
    fail_count INTEGER DEFAULT 0,

    first_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_checked_at DATETIME,
    last_healthy_at DATETIME,

    UNIQUE(host, port)
);
