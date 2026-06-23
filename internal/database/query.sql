-- name: UpsertProxy :one
INSERT INTO proxies (host, port, proxy_type, country, first_seen_at)
VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(host, port) DO UPDATE SET
    proxy_type = excluded.proxy_type,
    country = excluded.country
RETURNING *;

-- name: GetHealthyProxies :many
SELECT * FROM proxies
WHERE status = 'healthy'
ORDER BY last_healthy_at DESC;

-- name: GetProxyByHostPort :one
SELECT * FROM proxies
WHERE host = ? AND port = ?;

-- name: MarkProxyHealthy :exec
UPDATE proxies
SET status = ?, last_checked_at = CURRENT_TIMESTAMP, response_time_ms = ?,
    last_healthy_at = CURRENT_TIMESTAMP, fail_count = 0
WHERE id = ?;

-- name: MarkProxyUnhealthy :exec
UPDATE proxies
SET status = ?, last_checked_at = CURRENT_TIMESTAMP, response_time_ms = ?,
    fail_count = fail_count + 1
WHERE id = ?;

-- name: CleanupOldProxies :exec
DELETE FROM proxies
WHERE last_healthy_at IS NULL OR last_healthy_at < ?;

-- name: CountProxies :one
SELECT COUNT(*) FROM proxies;

-- name: CountHealthyProxies :one
SELECT COUNT(*) FROM proxies WHERE status = 'healthy';

-- name: CountProxiesByType :many
SELECT proxy_type, COUNT(*) AS count FROM proxies GROUP BY proxy_type;
