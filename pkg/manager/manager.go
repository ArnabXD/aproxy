package manager

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"aproxy/pkg/checker"
	"aproxy/pkg/scraper"
)

// ProxyManager defines the interface that proxy managers must implement
type ProxyManager interface {
	GetNextProxy() (*scraper.Proxy, error)
	GetRandomProxy() (*scraper.Proxy, error)
	ReportProxyFailure(scraper.Proxy)
	GetStats() Stats
	Start(updateInterval time.Duration) error
	Stop()
	RefreshProxies() error
}

type ProxyPool struct {
	proxies      []scraper.Proxy
	healthStatus map[string]checker.ProxyStatus
	lastChecked  map[string]time.Time
	failCount    map[string]int
	mu           sync.RWMutex
	currentIndex int
	maxFails     int
	recheckTime  time.Duration
}

type Manager struct {
	pool         *ProxyPool
	scraper      *scraper.MultiScraper
	checker      *checker.Checker
	updateTicker *time.Ticker
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		pool: &ProxyPool{
			proxies:      make([]scraper.Proxy, 0),
			healthStatus: make(map[string]checker.ProxyStatus),
			lastChecked:  make(map[string]time.Time),
			failCount:    make(map[string]int),
			maxFails:     3,
			recheckTime:  5 * time.Minute,
		},
		scraper: scraper.NewMultiScraper(),
		checker: checker.NewChecker(),
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (m *Manager) Start(updateInterval time.Duration) error {
	log.Println("Starting proxy manager...")

	if err := m.RefreshProxies(); err != nil {
		return fmt.Errorf("initial proxy refresh failed: %w", err)
	}

	m.updateTicker = time.NewTicker(updateInterval)

	m.wg.Add(1)
	go m.updateLoop()

	log.Printf("Proxy manager started with %d proxies", m.pool.Count())
	return nil
}

func (m *Manager) Stop() {
	log.Println("Stopping proxy manager...")

	if m.updateTicker != nil {
		m.updateTicker.Stop()
	}

	m.cancel()
	m.wg.Wait()

	log.Println("Proxy manager stopped")
}

func (m *Manager) RefreshProxies() error {
	log.Println("Refreshing proxy list...")

	ctx, cancel := context.WithTimeout(m.ctx, 2*time.Minute)
	defer cancel()

	proxies, err := m.scraper.ScrapeAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to scrape proxies: %w", err)
	}

	log.Printf("Scraped %d proxies, checking health...", len(proxies))

	results := m.checker.CheckProxies(ctx, proxies)
	healthyProxies := checker.FilterHealthyProxies(results)

	log.Printf("Found %d healthy proxies out of %d checked", len(healthyProxies), len(results))

	m.pool.mu.Lock()
	m.pool.proxies = healthyProxies
	m.pool.currentIndex = 0

	for _, result := range results {
		key := result.Proxy.Address()
		m.pool.healthStatus[key] = result.Status
		m.pool.lastChecked[key] = result.CheckedAt

		if result.Status != checker.StatusHealthy {
			m.pool.failCount[key]++
		} else {
			m.pool.failCount[key] = 0
		}
	}
	m.pool.mu.Unlock()

	return nil
}

func (m *Manager) GetNextProxy() (*scraper.Proxy, error) {
	m.pool.mu.Lock()
	defer m.pool.mu.Unlock()

	if len(m.pool.proxies) == 0 {
		return nil, fmt.Errorf("no healthy proxies available")
	}

	proxy := &m.pool.proxies[m.pool.currentIndex]
	m.pool.currentIndex = (m.pool.currentIndex + 1) % len(m.pool.proxies)

	return proxy, nil
}

func (m *Manager) GetRandomProxy() (*scraper.Proxy, error) {
	m.pool.mu.RLock()
	defer m.pool.mu.RUnlock()

	if len(m.pool.proxies) == 0 {
		return nil, fmt.Errorf("no healthy proxies available")
	}

	index := rand.Intn(len(m.pool.proxies))
	proxy := &m.pool.proxies[index]

	return proxy, nil
}

func (m *Manager) ReportProxyFailure(proxy scraper.Proxy) {
	m.pool.mu.Lock()
	defer m.pool.mu.Unlock()

	key := proxy.Address()
	m.pool.failCount[key]++
	m.pool.healthStatus[key] = checker.StatusUnhealthy

	if m.pool.failCount[key] >= m.pool.maxFails {
		m.removeProxy(proxy)
		log.Printf("Removed failing proxy: %s (failed %d times)", key, m.pool.failCount[key])
	}
}

func (m *Manager) removeProxy(targetProxy scraper.Proxy) {
	targetKey := targetProxy.Address()
	newProxies := make([]scraper.Proxy, 0, len(m.pool.proxies))

	for _, proxy := range m.pool.proxies {
		if proxy.Address() != targetKey {
			newProxies = append(newProxies, proxy)
		}
	}

	m.pool.proxies = newProxies

	if m.pool.currentIndex >= len(m.pool.proxies) && len(m.pool.proxies) > 0 {
		m.pool.currentIndex = 0
	}
}

func (m *Manager) GetStats() Stats {
	m.pool.mu.RLock()
	defer m.pool.mu.RUnlock()

	stats := Stats{
		TotalProxies: len(m.pool.proxies),
		HealthyCount: 0,
		TypeCount:    make(map[string]int),
		CountryCount: make(map[string]int),
	}

	for _, proxy := range m.pool.proxies {
		key := proxy.Address()
		if m.pool.healthStatus[key] == checker.StatusHealthy {
			stats.HealthyCount++
		}

		stats.TypeCount[proxy.Type]++
		if proxy.Country != "" {
			stats.CountryCount[proxy.Country]++
		}
	}

	return stats
}

func (m *Manager) updateLoop() {
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

func (p *ProxyPool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.proxies)
}

func (p *ProxyPool) HealthyCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	count := 0
	for _, proxy := range p.proxies {
		key := proxy.Address()
		if p.healthStatus[key] == checker.StatusHealthy {
			count++
		}
	}
	return count
}

type Stats struct {
	TotalProxies int
	HealthyCount int
	TypeCount    map[string]int
	CountryCount map[string]int
}
