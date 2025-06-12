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
CREATE INDEX IF NOT EXISTS idx_proxies_type ON proxies(proxy_type);

-- Proxy check history for detailed analytics (optional, for future use)
CREATE TABLE IF NOT EXISTS proxy_checks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    proxy_id INTEGER NOT NULL,
    status TEXT NOT NULL,
    response_time_ms INTEGER,
    error_message TEXT,
    checked_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    FOREIGN KEY (proxy_id) REFERENCES proxies (id) ON DELETE CASCADE
);

-- Index for proxy check history
CREATE INDEX IF NOT EXISTS idx_proxy_checks_proxy_id ON proxy_checks(proxy_id);
CREATE INDEX IF NOT EXISTS idx_proxy_checks_checked_at ON proxy_checks(checked_at);