package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"aproxy/internal/database/models/model"
	"aproxy/internal/database/models/table"
	"aproxy/pkg/scraper"

	. "github.com/go-jet/jet/v2/sqlite"
)

// Service handles database operations for proxies
type Service struct {
	db *DB
}

// NewService creates a new database service
func NewService(db *DB) *Service {
	return &Service{db: db}
}

// UpsertProxy inserts or updates a proxy in the database
func (s *Service) UpsertProxy(ctx context.Context, proxy scraper.Proxy) (*model.Proxies, error) {
	country := proxy.Country
	proxyModel := model.Proxies{
		Host:      proxy.Host,
		Port:      int32(proxy.Port),
		ProxyType: proxy.Type,
		Country:   &country,
		HTTPS:     nil, // Not available in scraper.Proxy
	}

	// Try to insert, if it fails due to unique constraint, update only metadata (preserve health data)
	now := time.Now()
	stmt := table.Proxies.INSERT(
		table.Proxies.Host,
		table.Proxies.Port,
		table.Proxies.ProxyType,
		table.Proxies.Country,
		table.Proxies.FirstSeenAt,
	).VALUES(
		proxyModel.Host,
		proxyModel.Port,
		proxyModel.ProxyType,
		proxyModel.Country,
		String(now.Format("2006-01-02 15:04:05")),
	).ON_CONFLICT(table.Proxies.Host, table.Proxies.Port).DO_UPDATE(SET(
		table.Proxies.ProxyType.SET(String(proxyModel.ProxyType)),
		table.Proxies.Country.SET(String(*proxyModel.Country)),
		// DO NOT update timestamps - preserve existing health check data
	)).RETURNING(table.Proxies.AllColumns)

	var result model.Proxies
	err := stmt.QueryContext(ctx, s.db, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to upsert proxy: %w", err)
	}

	return &result, nil
}

// GetProxiesNeedingCheck returns proxies that haven't been checked in the last checkInterval
func (s *Service) GetProxiesNeedingCheck(ctx context.Context, checkInterval time.Duration) ([]model.Proxies, error) {
	cutoff := time.Now().Add(-checkInterval)

	query := `
		SELECT id, host, port, proxy_type, country, anonymity, https, status, response_time_ms, fail_count, first_seen_at, last_checked_at, last_healthy_at 
		FROM proxies 
		WHERE last_checked_at IS NULL OR last_checked_at < ?
	`

	rows, err := s.db.QueryContext(ctx, query, cutoff.Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, fmt.Errorf("failed to get proxies needing check: %w", err)
	}
	defer rows.Close()

	var proxies []model.Proxies
	for rows.Next() {
		var p model.Proxies
		err := rows.Scan(
			&p.ID, &p.Host, &p.Port, &p.ProxyType, &p.Country, &p.Anonymity,
			&p.HTTPS, &p.Status, &p.ResponseTimeMs, &p.FailCount,
			&p.FirstSeenAt, &p.LastCheckedAt, &p.LastHealthyAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan proxy: %w", err)
		}
		proxies = append(proxies, p)
	}

	return proxies, nil
}

// BatchUpdateProxyHealth updates multiple proxy health statuses in a single transaction
func (s *Service) BatchUpdateProxyHealth(ctx context.Context, updates map[int32]CheckResult) error {
	if len(updates) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now()
	nowStr := now.Format("2006-01-02 15:04:05")

	// Prepare statements for healthy and unhealthy updates
	healthyQuery := `
		UPDATE proxies 
		SET status = ?, last_checked_at = ?, response_time_ms = ?, last_healthy_at = ?, fail_count = 0
		WHERE id = ?
	`
	unhealthyQuery := `
		UPDATE proxies 
		SET status = ?, last_checked_at = ?, response_time_ms = ?, fail_count = fail_count + 1
		WHERE id = ?
	`

	healthyStmt, err := tx.Prepare(healthyQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare healthy statement: %w", err)
	}
	defer healthyStmt.Close()

	unhealthyStmt, err := tx.Prepare(unhealthyQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare unhealthy statement: %w", err)
	}
	defer unhealthyStmt.Close()

	// Execute all updates
	for proxyID, result := range updates {
		if result.Status == StatusHealthy {
			_, err = healthyStmt.Exec(
				result.Status.String(),
				nowStr,
				int32(result.ResponseTime.Milliseconds()),
				nowStr,
				proxyID,
			)
		} else {
			_, err = unhealthyStmt.Exec(
				result.Status.String(),
				nowStr,
				int32(result.ResponseTime.Milliseconds()),
				proxyID,
			)
		}
		
		if err != nil {
			return fmt.Errorf("failed to update proxy %d: %w", proxyID, err)
		}
	}

	// Commit the transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("Batch updated %d proxy health records", len(updates))
	return nil
}

// GetHealthyProxies returns all healthy proxies
func (s *Service) GetHealthyProxies(ctx context.Context) ([]model.Proxies, error) {
	stmt := SELECT(
		table.Proxies.AllColumns,
	).FROM(
		table.Proxies,
	).WHERE(
		table.Proxies.Status.EQ(String("healthy")),
	).ORDER_BY(
		table.Proxies.LastHealthyAt.DESC(),
	)

	var proxies []model.Proxies
	err := stmt.QueryContext(ctx, s.db, &proxies)
	if err != nil {
		return nil, fmt.Errorf("failed to get healthy proxies: %w", err)
	}

	return proxies, nil
}

// GetProxiesByAddresses returns existing proxies for the given host:port addresses
func (s *Service) GetProxiesByAddresses(ctx context.Context, addresses []string) (map[string]*model.Proxies, error) {
	if len(addresses) == 0 {
		return make(map[string]*model.Proxies), nil
	}

	// Build the query with placeholders
	query := `
		SELECT id, host, port, proxy_type, country, anonymity, https, status, response_time_ms, fail_count, first_seen_at, last_checked_at, last_healthy_at
		FROM proxies 
		WHERE (host || ':' || port) IN (`

	args := make([]interface{}, len(addresses))
	placeholders := make([]string, len(addresses))
	for i, addr := range addresses {
		placeholders[i] = "?"
		args[i] = addr
	}
	query += strings.Join(placeholders, ",") + ")"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get proxies by addresses: %w", err)
	}
	defer rows.Close()

	result := make(map[string]*model.Proxies)
	for rows.Next() {
		var p model.Proxies
		err := rows.Scan(
			&p.ID, &p.Host, &p.Port, &p.ProxyType, &p.Country, &p.Anonymity,
			&p.HTTPS, &p.Status, &p.ResponseTimeMs, &p.FailCount,
			&p.FirstSeenAt, &p.LastCheckedAt, &p.LastHealthyAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan proxy: %w", err)
		}
		
		address := fmt.Sprintf("%s:%d", p.Host, p.Port)
		result[address] = &p
	}

	return result, nil
}

// GetProxyByHostPort finds a proxy by host and port
func (s *Service) GetProxyByHostPort(ctx context.Context, host string, port int) (*model.Proxies, error) {
	stmt := SELECT(
		table.Proxies.AllColumns,
	).FROM(
		table.Proxies,
	).WHERE(
		table.Proxies.Host.EQ(String(host)).
			AND(table.Proxies.Port.EQ(Int32(int32(port)))),
	)

	var proxy model.Proxies
	err := stmt.QueryContext(ctx, s.db, &proxy)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get proxy: %w", err)
	}

	return &proxy, nil
}

// CleanupOldProxies removes proxies that haven't been healthy for a long time
func (s *Service) CleanupOldProxies(ctx context.Context, maxAge time.Duration) error {
	cutoff := time.Now().Add(-maxAge)

	query := `DELETE FROM proxies WHERE last_healthy_at IS NULL OR last_healthy_at < ?`

	_, err := s.db.ExecContext(ctx, query, cutoff.Format("2006-01-02 15:04:05"))
	if err != nil {
		return fmt.Errorf("failed to cleanup old proxies: %w", err)
	}

	return nil
}

// GetProxyStats returns statistics about the proxy database
func (s *Service) GetProxyStats(ctx context.Context) (ProxyStats, error) {
	var stats ProxyStats

	// Count total proxies using raw SQL
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM proxies").Scan(&stats.Total)
	if err != nil {
		return stats, fmt.Errorf("failed to count total proxies: %w", err)
	}

	// Count healthy proxies using raw SQL
	err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM proxies WHERE status = 'healthy'").Scan(&stats.Healthy)
	if err != nil {
		return stats, fmt.Errorf("failed to count healthy proxies: %w", err)
	}

	// Count by type using raw SQL
	rows, err := s.db.QueryContext(ctx, "SELECT proxy_type, COUNT(*) FROM proxies GROUP BY proxy_type")
	if err != nil {
		return stats, fmt.Errorf("failed to get proxy types: %w", err)
	}
	defer rows.Close()

	stats.ByType = make(map[string]int)
	for rows.Next() {
		var proxyType string
		var count int
		if err := rows.Scan(&proxyType, &count); err != nil {
			return stats, fmt.Errorf("failed to scan proxy type row: %w", err)
		}
		stats.ByType[proxyType] = count
	}

	return stats, nil
}

// ProxyStats contains statistics about the proxy database
type ProxyStats struct {
	Total   int            `json:"total"`
	Healthy int            `json:"healthy"`
	ByType  map[string]int `json:"by_type"`
}
