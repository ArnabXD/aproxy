package checker

import (
	"context"
	"fmt"
	"log"
	"time"

	"aproxy/internal/database"
	"aproxy/internal/database/models/model"
	"aproxy/pkg/scraper"
)

// DBChecker is a checker that uses SQLite for caching proxy health status
type DBChecker struct {
	*Checker
	dbService     *database.Service
	checkInterval time.Duration
}

// NewDBChecker creates a new database-backed checker
func NewDBChecker(dbService *database.Service, checkInterval time.Duration) *DBChecker {
	return &DBChecker{
		Checker:       NewChecker(),
		dbService:     dbService,
		checkInterval: checkInterval,
	}
}


// CheckProxiesWithCaching checks proxies but skips those checked recently
func (c *DBChecker) CheckProxiesWithCaching(ctx context.Context, proxies []scraper.Proxy) []CheckResult {
	if len(proxies) == 0 {
		return nil
	}

	log.Printf("Checking %d proxies with caching (skip if checked within %v)", len(proxies), c.checkInterval)

	// Get addresses of all scraped proxies
	addresses := make([]string, len(proxies))
	proxyByAddr := make(map[string]scraper.Proxy)
	for i, proxy := range proxies {
		addr := proxy.Address()
		addresses[i] = addr
		proxyByAddr[addr] = proxy
	}

	// Get existing proxies from database
	existingProxies, err := c.dbService.GetProxiesByAddresses(ctx, addresses)
	if err != nil {
		log.Printf("Failed to get existing proxies: %v", err)
		// Fall back to checking all proxies
		return c.Checker.CheckProxies(ctx, proxies)
	}

	log.Printf("Found %d existing proxies in database", len(existingProxies))

	// Separate new proxies that need to be inserted vs existing ones
	var newProxies []scraper.Proxy
	var dbProxies []*model.Proxies
	cutoff := time.Now().Add(-c.checkInterval)

	for addr, proxy := range proxyByAddr {
		if dbProxy, exists := existingProxies[addr]; exists {
			// Proxy exists in database
			dbProxies = append(dbProxies, dbProxy)
		} else {
			// New proxy, needs to be inserted
			newProxies = append(newProxies, proxy)
		}
	}

	// Upsert only new proxies
	for _, proxy := range newProxies {
		dbProxy, err := c.dbService.UpsertProxy(ctx, proxy)
		if err != nil {
			log.Printf("Failed to upsert new proxy %s: %v", proxy.Address(), err)
			continue
		}
		dbProxies = append(dbProxies, dbProxy)
	}

	// Determine which proxies need health checks
	var proxiesToCheck []scraper.Proxy
	var proxiesNeedingCheck []*model.Proxies
	
	for _, dbProxy := range dbProxies {
		needsCheck := false
		if dbProxy.LastCheckedAt == nil {
			needsCheck = true
		} else {
			needsCheck = dbProxy.LastCheckedAt.Before(cutoff)
		}

		if needsCheck {
			proxy := scraper.Proxy{
				Host:     dbProxy.Host,
				Port:     int(dbProxy.Port),
				Type:     dbProxy.ProxyType,
				Country:  "",
				LastSeen: time.Now(),
			}
			if dbProxy.Country != nil {
				proxy.Country = *dbProxy.Country
			}
			
			proxiesToCheck = append(proxiesToCheck, proxy)
			proxiesNeedingCheck = append(proxiesNeedingCheck, dbProxy)
		}
	}

	log.Printf("Found %d proxies that need checking (out of %d total)", len(proxiesToCheck), len(dbProxies))

	if len(proxiesToCheck) == 0 {
		// All proxies have been checked recently, return cached results
		return c.getCachedResults(ctx, dbProxies)
	}

	// Check the proxies that need checking
	results := c.Checker.CheckProxies(ctx, proxiesToCheck)

	// Batch update database with all results in a single transaction
	proxyMap := make(map[string]*model.Proxies)
	for _, dbProxy := range proxiesNeedingCheck {
		addr := fmt.Sprintf("%s:%d", dbProxy.Host, dbProxy.Port)
		proxyMap[addr] = dbProxy
	}

	// Prepare batch updates
	updates := make(map[int32]database.CheckResult)
	for _, result := range results {
		proxyAddr := result.Proxy.Address()
		dbProxy, exists := proxyMap[proxyAddr]
		if !exists {
			log.Printf("No database proxy found for %s", proxyAddr)
			continue
		}

		// Convert checker.CheckResult to database.CheckResult
		dbResult := database.CheckResult{
			Proxy:        result.Proxy,
			Status:       database.ProxyStatus(result.Status),
			ResponseTime: result.ResponseTime,
			Error:        result.Error,
			CheckedAt:    result.CheckedAt,
		}

		updates[*dbProxy.ID] = dbResult
	}

	// Execute batch update
	if len(updates) > 0 {
		updateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := c.dbService.BatchUpdateProxyHealth(updateCtx, updates); err != nil {
			log.Printf("Failed to batch update proxy health: %v", err)
		}
		cancel()
	}

	// Return results for all proxies (mix of fresh checks and cached results)
	return c.getAllResults(ctx, dbProxies, results)
}

// getCachedResults returns cached check results for all proxies
func (c *DBChecker) getCachedResults(ctx context.Context, dbProxies []*model.Proxies) []CheckResult {
	var results []CheckResult

	for _, dbProxy := range dbProxies {
		proxy := scraper.Proxy{
			Host:    dbProxy.Host,
			Port:    int(dbProxy.Port),
			Type:    dbProxy.ProxyType,
			Country: "",
		}
		if dbProxy.Country != nil {
			proxy.Country = *dbProxy.Country
		}

		status := StatusUnknown
		switch dbProxy.Status {
		case "healthy":
			status = StatusHealthy
		case "unhealthy":
			status = StatusUnhealthy
		case "timeout":
			status = StatusTimeout
		case "error":
			status = StatusError
		}

		result := CheckResult{
			Proxy:  proxy,
			Status: status,
		}

		if dbProxy.LastCheckedAt != nil {
			result.CheckedAt = *dbProxy.LastCheckedAt
		}

		if dbProxy.ResponseTimeMs != nil {
			result.ResponseTime = time.Duration(*dbProxy.ResponseTimeMs) * time.Millisecond
		}

		results = append(results, result)
	}

	return results
}

// getAllResults combines fresh check results with cached results
func (c *DBChecker) getAllResults(ctx context.Context, dbProxies []*model.Proxies, freshResults []CheckResult) []CheckResult {
	// Create a map of fresh results by proxy address
	freshMap := make(map[string]CheckResult)
	for _, result := range freshResults {
		freshMap[result.Proxy.Address()] = result
	}

	var allResults []CheckResult

	for _, dbProxy := range dbProxies {
		proxyAddr := fmt.Sprintf("%s:%d", dbProxy.Host, dbProxy.Port)

		// Use fresh result if available, otherwise use cached result
		if freshResult, exists := freshMap[proxyAddr]; exists {
			allResults = append(allResults, freshResult)
		} else {
			// Create cached result
			proxy := scraper.Proxy{
				Host:    dbProxy.Host,
				Port:    int(dbProxy.Port),
				Type:    dbProxy.ProxyType,
				Country: "",
			}
			if dbProxy.Country != nil {
				proxy.Country = *dbProxy.Country
			}

			status := StatusUnknown
			switch dbProxy.Status {
			case "healthy":
				status = StatusHealthy
			case "unhealthy":
				status = StatusUnhealthy
			case "timeout":
				status = StatusTimeout
			case "error":
				status = StatusError
			}

			result := CheckResult{
				Proxy:  proxy,
				Status: status,
			}

			if dbProxy.LastCheckedAt != nil {
				result.CheckedAt = *dbProxy.LastCheckedAt
			}

			if dbProxy.ResponseTimeMs != nil {
				result.ResponseTime = time.Duration(*dbProxy.ResponseTimeMs) * time.Millisecond
			}

			allResults = append(allResults, result)
		}
	}

	return allResults
}

// GetHealthyProxiesFromDB returns healthy proxies from the database
func (c *DBChecker) GetHealthyProxiesFromDB(ctx context.Context) ([]scraper.Proxy, error) {
	dbProxies, err := c.dbService.GetHealthyProxies(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get healthy proxies from database: %w", err)
	}

	proxies := make([]scraper.Proxy, 0, len(dbProxies))
	for _, dbProxy := range dbProxies {
		proxy := scraper.Proxy{
			Host:    dbProxy.Host,
			Port:    int(dbProxy.Port),
			Type:    dbProxy.ProxyType,
			Country: "",
		}
		if dbProxy.Country != nil {
			proxy.Country = *dbProxy.Country
		}
		if dbProxy.LastHealthyAt != nil {
			proxy.LastSeen = *dbProxy.LastHealthyAt
		}

		proxies = append(proxies, proxy)
	}

	return proxies, nil
}

// CleanupOldProxies removes proxies that haven't been healthy for a long time
func (c *DBChecker) CleanupOldProxies(ctx context.Context, maxAge time.Duration) error {
	return c.dbService.CleanupOldProxies(ctx, maxAge)
}

// GetStats returns database proxy statistics
func (c *DBChecker) GetStats(ctx context.Context) (database.ProxyStats, error) {
	return c.dbService.GetProxyStats(ctx)
}
