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
	batchSize     int
	batchDelay    time.Duration
}

// NewDBChecker creates a new database-backed checker
func NewDBChecker(dbService *database.Service, checkInterval time.Duration, batchSize int, batchDelay time.Duration) *DBChecker {
	return &DBChecker{
		Checker:       NewChecker(),
		dbService:     dbService,
		checkInterval: checkInterval,
		batchSize:     batchSize,
		batchDelay:    batchDelay,
	}
}

// NewDBCheckerWithConfig creates a new database-backed checker with configuration
func NewDBCheckerWithConfig(dbService *database.Service, checkerConfig CheckerConfig, checkInterval time.Duration, batchSize int, batchDelay time.Duration) *DBChecker {
	return &DBChecker{
		Checker:       NewCheckerWithConfig(checkerConfig),
		dbService:     dbService,
		checkInterval: checkInterval,
		batchSize:     batchSize,
		batchDelay:    batchDelay,
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

	// Check the proxies that need checking (progressively in batches)
	results := c.checkProxiesProgressive(ctx, proxiesToCheck)

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

	// Execute batch update in smaller chunks to avoid timeouts
	if len(updates) > 0 {
		const maxBatchSize = 50 // Smaller batch size for faster commits
		
		// Process updates in smaller batches
		updateKeys := make([]int32, 0, len(updates))
		for id := range updates {
			updateKeys = append(updateKeys, id)
		}
		
		for i := 0; i < len(updateKeys); i += maxBatchSize {
			end := i + maxBatchSize
			if end > len(updateKeys) {
				end = len(updateKeys)
			}
			
			// Create smaller batch
			batchUpdates := make(map[int32]database.CheckResult)
			for j := i; j < end; j++ {
				id := updateKeys[j]
				batchUpdates[id] = updates[id]
			}
			
			// Use longer timeout for database operations
			updateCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := c.dbService.BatchUpdateProxyHealth(updateCtx, batchUpdates); err != nil {
				log.Printf("Failed to batch update proxy health (batch %d-%d): %v", i, end-1, err)
			} else {
				log.Printf("Successfully updated %d proxy health records to database", len(batchUpdates))
			}
			cancel()
		}
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

// checkProxiesProgressive checks proxies in batches with delays to avoid overwhelming the system
func (c *DBChecker) checkProxiesProgressive(ctx context.Context, proxies []scraper.Proxy) []CheckResult {
	if len(proxies) == 0 {
		return nil
	}

	var allResults []CheckResult
	totalBatches := (len(proxies) + c.batchSize - 1) / c.batchSize

	log.Printf("Checking %d proxies in %d batches (batch size: %d, delay: %v)", 
		len(proxies), totalBatches, c.batchSize, c.batchDelay)

	for i := 0; i < len(proxies); i += c.batchSize {
		// Check for cancellation before starting each batch
		select {
		case <-ctx.Done():
			log.Printf("Context cancelled before batch %d/%d, stopping progressive checking", i/c.batchSize+1, totalBatches)
			return allResults
		default:
			// Continue with batch
		}

		// Create batch
		end := i + c.batchSize
		if end > len(proxies) {
			end = len(proxies)
		}
		batch := proxies[i:end]
		batchNum := i/c.batchSize + 1

		log.Printf("Checking batch %d/%d (%d proxies)", batchNum, totalBatches, len(batch))

		// Check batch using original checker
		batchResults := c.Checker.CheckProxies(ctx, batch)
		allResults = append(allResults, batchResults...)

		// Save this batch's results to database immediately (but only if context is still active)
		if len(batchResults) > 0 {
			select {
			case <-ctx.Done():
				// Context cancelled, skip background save
				log.Printf("Context cancelled, skipping database save for batch %d", batchNum)
			default:
				// Context still active, save in background
				go func(results []CheckResult, batchNumber int) {
					// Use a short timeout for the background save operation
					saveCtx, saveCancel := context.WithTimeout(context.Background(), 15*time.Second)
					defer saveCancel()

					// Update database with this batch's results
					addresses := make([]string, len(results))
					for i, result := range results {
						addresses[i] = result.Proxy.Address()
					}
					
					if dbProxies, err := c.dbService.GetProxiesByAddresses(saveCtx, addresses); err == nil {
						updates := make(map[int32]database.CheckResult)
						for _, result := range results {
							if dbProxy, exists := dbProxies[result.Proxy.Address()]; exists && dbProxy.ID != nil {
								updates[*dbProxy.ID] = database.CheckResult{
									Proxy:        result.Proxy,
									Status:       database.ProxyStatus(result.Status),
									ResponseTime: result.ResponseTime,
									Error:        result.Error,
									CheckedAt:    result.CheckedAt,
								}
							}
						}
						if len(updates) > 0 {
							if err := c.dbService.BatchUpdateProxyHealth(saveCtx, updates); err == nil {
								log.Printf("Saved batch %d results to database (%d records)", batchNumber, len(updates))
							}
						}
					}
				}(batchResults, batchNum)
			}
		}

		// Log progress with healthy count so far
		healthyCount := 0
		for _, result := range allResults {
			if result.Status == StatusHealthy {
				healthyCount++
			}
		}
		log.Printf("Batch %d/%d complete. Total healthy so far: %d", batchNum, totalBatches, healthyCount)

		// Add delay between batches (except for the last one)
		if end < len(proxies) {
			select {
			case <-ctx.Done():
				log.Printf("Context cancelled, stopping progressive checking at batch %d/%d with %d healthy proxies found", batchNum, totalBatches, healthyCount)
				return allResults
			case <-time.After(c.batchDelay):
				// Continue to next batch
			}
		}
	}

	healthyCount := 0
	for _, result := range allResults {
		if result.Status == StatusHealthy {
			healthyCount++
		}
	}
	log.Printf("Progressive checking completed: checked %d proxies in %d batches, found %d healthy", len(proxies), totalBatches, healthyCount)
	return allResults
}

// GetStats returns database proxy statistics
func (c *DBChecker) GetStats(ctx context.Context) (database.ProxyStats, error) {
	return c.dbService.GetProxyStats(ctx)
}
