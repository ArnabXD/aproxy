package manager

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"aproxy/internal/database"
	"aproxy/pkg/checker"
	"aproxy/pkg/scraper"
)

// DBManager is a manager that uses SQLite for persistent proxy storage
type DBManager struct {
	scraper      *scraper.MultiScraper
	dbChecker    *checker.DBChecker
	dbService    *database.Service
	updateTicker *time.Ticker
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup

	// In-memory cache for fast access
	cachedProxies []scraper.Proxy
	currentIndex  int
	mu            sync.RWMutex

	// Configuration
	backgroundEnabled bool
	updateInterval    time.Duration
}

// NewDBManager creates a new database-backed manager
func NewDBManager(db *database.DB, checkInterval time.Duration, backgroundEnabled bool, batchSize int, batchDelay time.Duration) *DBManager {
	ctx, cancel := context.WithCancel(context.Background())

	dbService := database.NewService(db)
	dbChecker := checker.NewDBChecker(dbService, checkInterval, batchSize, batchDelay)

	return &DBManager{
		scraper:           scraper.NewMultiScraper(),
		dbChecker:         dbChecker,
		dbService:         dbService,
		ctx:               ctx,
		cancel:            cancel,
		cachedProxies:     make([]scraper.Proxy, 0),
		backgroundEnabled: backgroundEnabled,
	}
}

// Start begins the proxy manager operations with non-blocking startup
func (m *DBManager) Start(updateInterval time.Duration) error {
	log.Println("Starting database-backed proxy manager...")
	m.updateInterval = updateInterval

	// Load existing healthy proxies from database (fast, non-blocking)
	if err := m.loadHealthyProxies(); err != nil {
		log.Printf("Failed to load existing proxies: %v", err)
	}

	log.Printf("Database proxy manager started with %d cached proxies", len(m.cachedProxies))

	// Start background operations if enabled
	if m.backgroundEnabled {
		log.Println("Starting background proxy operations...")
		
		// Start background refresh immediately if we have no proxies
		if len(m.cachedProxies) == 0 {
			log.Println("No cached proxies found, starting immediate background refresh...")
			m.wg.Add(1)
			go m.backgroundRefresh()
		}

		// Start periodic update loop
		m.updateTicker = time.NewTicker(updateInterval)
		m.wg.Add(1)
		go m.updateLoop()
		
		// Start periodic cache refresh from database (more frequent than full updates)
		cacheRefreshTicker := time.NewTicker(1 * time.Minute)
		m.wg.Add(1)
		go m.cacheRefreshLoop(cacheRefreshTicker)
	} else {
		log.Println("Background checking disabled, running initial refresh...")
		// Fallback to blocking behavior if background is disabled
		if err := m.RefreshProxies(); err != nil {
			return fmt.Errorf("initial proxy refresh failed: %w", err)
		}
	}

	return nil
}

// Stop stops the proxy manager
func (m *DBManager) Stop() {
	log.Println("Stopping database proxy manager...")

	if m.updateTicker != nil {
		m.updateTicker.Stop()
	}

	m.cancel()
	m.wg.Wait()

	log.Println("Database proxy manager stopped")
}

// RefreshProxies scrapes new proxies and checks them with caching
func (m *DBManager) RefreshProxies() error {
	log.Println("Refreshing proxy list with database caching...")

	// Use manager's context to respect cancellation, but with timeout
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Minute)
	defer cancel()

	// Scrape fresh proxies
	proxies, err := m.scraper.ScrapeAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to scrape proxies: %w", err)
	}

	log.Printf("Scraped %d proxies, checking health with caching...", len(proxies))

	// Use database-backed checker with caching and progressive updates
	results := m.dbChecker.CheckProxiesWithCaching(ctx, proxies)
	healthyProxies := checker.FilterHealthyProxies(results)

	log.Printf("Found %d healthy proxies out of %d checked", len(healthyProxies), len(results))

	// Update in-memory cache
	m.mu.Lock()
	oldCount := len(m.cachedProxies)
	m.cachedProxies = healthyProxies
	m.currentIndex = 0
	newCount := len(m.cachedProxies)
	m.mu.Unlock()

	log.Printf("Updated proxy cache: %d -> %d healthy proxies", oldCount, newCount)

	// If we have healthy proxies now but had none before, reload from database as well
	if oldCount == 0 && newCount > 0 {
		log.Println("Cache was empty but now has proxies, reloading from database to include any existing healthy proxies")
		if err := m.loadHealthyProxies(); err != nil {
			log.Printf("Failed to reload from database: %v", err)
		} else {
			m.mu.RLock()
			finalCount := len(m.cachedProxies)
			m.mu.RUnlock()
			log.Printf("Final cache count after database reload: %d proxies", finalCount)
		}
	}

	// Cleanup old proxies in the background
	go func() {
		if err := m.dbChecker.CleanupOldProxies(context.Background(), 24*time.Hour); err != nil {
			log.Printf("Failed to cleanup old proxies: %v", err)
		}
	}()

	return nil
}

// loadHealthyProxies loads existing healthy proxies from database into cache
func (m *DBManager) loadHealthyProxies() error {
	ctx := context.Background()
	proxies, err := m.dbChecker.GetHealthyProxiesFromDB(ctx)
	if err != nil {
		return fmt.Errorf("failed to load healthy proxies: %w", err)
	}

	m.mu.Lock()
	m.cachedProxies = proxies
	m.currentIndex = 0
	m.mu.Unlock()

	log.Printf("Loaded %d healthy proxies from database", len(proxies))
	return nil
}

// GetNextProxy returns the next proxy in round-robin fashion
func (m *DBManager) GetNextProxy() (*scraper.Proxy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.cachedProxies) == 0 {
		return nil, fmt.Errorf("no healthy proxies available")
	}

	proxy := &m.cachedProxies[m.currentIndex]
	m.currentIndex = (m.currentIndex + 1) % len(m.cachedProxies)

	return proxy, nil
}

// GetRandomProxy returns a random proxy
func (m *DBManager) GetRandomProxy() (*scraper.Proxy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.cachedProxies) == 0 {
		return nil, fmt.Errorf("no healthy proxies available")
	}

	index := rand.Intn(len(m.cachedProxies))
	proxy := &m.cachedProxies[index]

	return proxy, nil
}

// ReportProxyFailure removes a failing proxy from the cache
func (m *DBManager) ReportProxyFailure(proxy scraper.Proxy) {
	m.mu.Lock()
	defer m.mu.Unlock()

	targetKey := proxy.Address()
	newProxies := make([]scraper.Proxy, 0, len(m.cachedProxies))

	for _, p := range m.cachedProxies {
		if p.Address() != targetKey {
			newProxies = append(newProxies, p)
		}
	}

	if len(newProxies) < len(m.cachedProxies) {
		m.cachedProxies = newProxies
		log.Printf("Removed failing proxy from cache: %s", targetKey)

		if m.currentIndex >= len(m.cachedProxies) && len(m.cachedProxies) > 0 {
			m.currentIndex = 0
		}
	}
}

// GetStats returns database and cache statistics
func (m *DBManager) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := Stats{
		TotalProxies: len(m.cachedProxies),
		HealthyCount: len(m.cachedProxies),
		TypeCount:    make(map[string]int),
		CountryCount: make(map[string]int),
	}

	for _, proxy := range m.cachedProxies {
		stats.TypeCount[proxy.Type]++
		if proxy.Country != "" {
			stats.CountryCount[proxy.Country]++
		}
	}

	log.Printf("DEBUG: GetStats() called - cached proxies: %d", len(m.cachedProxies))
	return stats
}

// GetDBStats returns detailed database statistics
func (m *DBManager) GetDBStats(ctx context.Context) (database.ProxyStats, error) {
	return m.dbChecker.GetStats(ctx)
}

// backgroundRefresh runs an immediate background refresh (for startup with no proxies)
func (m *DBManager) backgroundRefresh() {
	defer m.wg.Done()
	
	log.Println("Running background refresh...")
	if err := m.RefreshProxies(); err != nil {
		log.Printf("Background refresh failed: %v", err)
		// If refresh failed, try to load any existing healthy proxies from DB
		log.Println("Attempting to load healthy proxies from database as fallback...")
		if err := m.loadHealthyProxies(); err != nil {
			log.Printf("Fallback load also failed: %v", err)
		}
	}
}

// cacheRefreshLoop periodically reloads healthy proxies from database
func (m *DBManager) cacheRefreshLoop(ticker *time.Ticker) {
	defer m.wg.Done()
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			// Only reload if we have few proxies in cache
			m.mu.RLock()
			currentCount := len(m.cachedProxies)
			m.mu.RUnlock()
			
			if currentCount < 5 { // Reload if we have fewer than 5 proxies
				log.Printf("Cache has only %d proxies, reloading from database...", currentCount)
				if err := m.loadHealthyProxies(); err != nil {
					log.Printf("Failed to reload cache from database: %v", err)
				} else {
					m.mu.RLock()
					newCount := len(m.cachedProxies)
					m.mu.RUnlock()
					if newCount > currentCount {
						log.Printf("Cache reloaded: %d -> %d proxies", currentCount, newCount)
					}
				}
			}
		}
	}
}

// updateLoop runs the periodic proxy refresh
func (m *DBManager) updateLoop() {
	defer m.wg.Done()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.updateTicker.C:
			log.Println("Running scheduled proxy refresh...")
			if err := m.RefreshProxies(); err != nil {
				log.Printf("Failed to refresh proxies: %v", err)
			}
		}
	}
}

// Count returns the number of cached proxies
func (m *DBManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.cachedProxies)
}

// HealthyCount returns the number of healthy proxies (same as Count for DBManager)
func (m *DBManager) HealthyCount() int {
	return m.Count()
}
