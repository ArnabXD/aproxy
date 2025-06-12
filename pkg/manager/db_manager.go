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
}

// NewDBManager creates a new database-backed manager
func NewDBManager(db *database.DB, checkInterval time.Duration) *DBManager {
	ctx, cancel := context.WithCancel(context.Background())

	dbService := database.NewService(db)
	dbChecker := checker.NewDBChecker(dbService, checkInterval)

	return &DBManager{
		scraper:       scraper.NewMultiScraper(),
		dbChecker:     dbChecker,
		dbService:     dbService,
		ctx:           ctx,
		cancel:        cancel,
		cachedProxies: make([]scraper.Proxy, 0),
	}
}

// Start begins the proxy manager operations
func (m *DBManager) Start(updateInterval time.Duration) error {
	log.Println("Starting database-backed proxy manager...")

	// Load existing healthy proxies from database
	if err := m.loadHealthyProxies(); err != nil {
		log.Printf("Failed to load existing proxies: %v", err)
	}

	// Do initial refresh
	if err := m.RefreshProxies(); err != nil {
		return fmt.Errorf("initial proxy refresh failed: %w", err)
	}

	m.updateTicker = time.NewTicker(updateInterval)

	m.wg.Add(1)
	go m.updateLoop()

	log.Printf("Database proxy manager started with %d cached proxies", len(m.cachedProxies))
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

	ctx, cancel := context.WithTimeout(m.ctx, 3*time.Minute)
	defer cancel()

	// Scrape fresh proxies
	proxies, err := m.scraper.ScrapeAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to scrape proxies: %w", err)
	}

	log.Printf("Scraped %d proxies, checking health with caching...", len(proxies))

	// Use database-backed checker with caching
	results := m.dbChecker.CheckProxiesWithCaching(ctx, proxies)
	healthyProxies := checker.FilterHealthyProxies(results)

	log.Printf("Found %d healthy proxies out of %d checked", len(healthyProxies), len(results))

	// Update in-memory cache
	m.mu.Lock()
	m.cachedProxies = healthyProxies
	m.currentIndex = 0
	m.mu.Unlock()

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

	return stats
}

// GetDBStats returns detailed database statistics
func (m *DBManager) GetDBStats(ctx context.Context) (database.ProxyStats, error) {
	return m.dbChecker.GetStats(ctx)
}

// updateLoop runs the periodic proxy refresh
func (m *DBManager) updateLoop() {
	defer m.wg.Done()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.updateTicker.C:
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
