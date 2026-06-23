package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"aproxy/internal/database/db"
	"aproxy/pkg/scraper"
)

// Proxy is the stored proxy row (re-exported sqlc model).
type Proxy = db.Proxy

// Service handles database operations for proxies.
type Service struct {
	q  *db.Queries
	db *DB
}

// NewService creates a new database service.
func NewService(database *DB) *Service {
	return &Service{q: db.New(database), db: database}
}

// UpsertProxy inserts a proxy or, on host:port conflict, refreshes its metadata
// (preserving health/timestamp columns).
func (s *Service) UpsertProxy(ctx context.Context, proxy scraper.Proxy) (*Proxy, error) {
	country := proxy.Country
	p, err := s.q.UpsertProxy(ctx, db.UpsertProxyParams{
		Host:      proxy.Host,
		Port:      int64(proxy.Port),
		ProxyType: proxy.Type,
		Country:   &country,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upsert proxy: %w", err)
	}
	return &p, nil
}

// GetHealthyProxies returns all healthy proxies.
func (s *Service) GetHealthyProxies(ctx context.Context) ([]Proxy, error) {
	proxies, err := s.q.GetHealthyProxies(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get healthy proxies: %w", err)
	}
	return proxies, nil
}

// GetProxyByHostPort finds a proxy by host and port, or returns (nil, nil).
func (s *Service) GetProxyByHostPort(ctx context.Context, host string, port int) (*Proxy, error) {
	p, err := s.q.GetProxyByHostPort(ctx, db.GetProxyByHostPortParams{Host: host, Port: int64(port)})
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get proxy: %w", err)
	}
	return &p, nil
}

// GetProxiesByAddresses returns existing proxies for the given host:port keys.
// Hand-written: sqlc's sqlite engine doesn't support sqlc.slice() for IN lists.
func (s *Service) GetProxiesByAddresses(ctx context.Context, addresses []string) (map[string]*Proxy, error) {
	result := make(map[string]*Proxy)
	if len(addresses) == 0 {
		return result, nil
	}

	args := make([]any, len(addresses))
	placeholders := make([]string, len(addresses))
	for i, addr := range addresses {
		placeholders[i] = "?"
		args[i] = addr
	}
	query := `SELECT id, host, port, proxy_type, country, anonymity, https, status,
		response_time_ms, fail_count, first_seen_at, last_checked_at, last_healthy_at
		FROM proxies WHERE (host || ':' || port) IN (` + strings.Join(placeholders, ",") + ")"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get proxies by addresses: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var p Proxy
		if err := rows.Scan(
			&p.ID, &p.Host, &p.Port, &p.ProxyType, &p.Country, &p.Anonymity,
			&p.Https, &p.Status, &p.ResponseTimeMs, &p.FailCount,
			&p.FirstSeenAt, &p.LastCheckedAt, &p.LastHealthyAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan proxy: %w", err)
		}
		result[fmt.Sprintf("%s:%d", p.Host, p.Port)] = &p
	}
	return result, rows.Err()
}

// BatchUpdateProxyHealth updates many proxies' health in one transaction.
func (s *Service) BatchUpdateProxyHealth(ctx context.Context, updates map[int32]CheckResult) error {
	if len(updates) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := s.q.WithTx(tx)
	for id, result := range updates {
		rt := int64(result.ResponseTime.Milliseconds())
		if result.Status == StatusHealthy {
			err = qtx.MarkProxyHealthy(ctx, db.MarkProxyHealthyParams{
				Status: result.Status.String(), ResponseTimeMs: &rt, ID: int64(id),
			})
		} else {
			err = qtx.MarkProxyUnhealthy(ctx, db.MarkProxyUnhealthyParams{
				Status: result.Status.String(), ResponseTimeMs: &rt, ID: int64(id),
			})
		}
		if err != nil {
			return fmt.Errorf("failed to update proxy %d: %w", id, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// CleanupOldProxies removes proxies that haven't been healthy since maxAge ago.
func (s *Service) CleanupOldProxies(ctx context.Context, maxAge time.Duration) error {
	cutoff := time.Now().Add(-maxAge)
	if err := s.q.CleanupOldProxies(ctx, &cutoff); err != nil {
		return fmt.Errorf("failed to cleanup old proxies: %w", err)
	}
	return nil
}

// GetProxyStats returns aggregate statistics about the proxy table.
func (s *Service) GetProxyStats(ctx context.Context) (ProxyStats, error) {
	var stats ProxyStats

	total, err := s.q.CountProxies(ctx)
	if err != nil {
		return stats, fmt.Errorf("failed to count total proxies: %w", err)
	}
	stats.Total = int(total)

	healthy, err := s.q.CountHealthyProxies(ctx)
	if err != nil {
		return stats, fmt.Errorf("failed to count healthy proxies: %w", err)
	}
	stats.Healthy = int(healthy)

	byType, err := s.q.CountProxiesByType(ctx)
	if err != nil {
		return stats, fmt.Errorf("failed to count proxies by type: %w", err)
	}
	stats.ByType = make(map[string]int, len(byType))
	for _, row := range byType {
		stats.ByType[row.ProxyType] = int(row.Count)
	}

	return stats, nil
}

// ProxyStats contains statistics about the proxy database.
type ProxyStats struct {
	Total   int            `json:"total"`
	Healthy int            `json:"healthy"`
	ByType  map[string]int `json:"by_type"`
}
