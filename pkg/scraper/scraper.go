package scraper

import (
	"aproxy/internal/logger"
	"context"
)

type MultiScraper struct {
	scrapers []Scraper
	logger   *logger.Logger
}

func NewMultiScraperWithConfig(config ScraperConfig) *MultiScraper {
	enabled := make(map[string]bool, len(config.Sources))
	for _, s := range config.Sources {
		enabled[s] = true
	}

	var scrapers []Scraper
	for _, src := range sources {
		if len(enabled) == 0 || enabled[src.name] {
			scrapers = append(scrapers, newListScraper(src, config))
		}
	}

	return &MultiScraper{
		scrapers: scrapers,
		logger:   logger.New("multiscraper"),
	}
}

func (m *MultiScraper) ScrapeAll(ctx context.Context) ([]Proxy, error) {
	var allProxies []Proxy
	seen := make(map[string]bool)

	for _, scraper := range m.scrapers {
		proxies, err := scraper.Scrape(ctx)
		if err != nil {
			m.logger.WarnBg("Scraper %s failed: %v", scraper.Name(), err)
			continue
		}

		unique := 0
		for _, proxy := range proxies {
			key := proxy.Address()
			if !seen[key] {
				seen[key] = true
				allProxies = append(allProxies, proxy)
				unique++
			}
		}
		m.logger.InfoBg("Scraper %s: %d total, %d unique", scraper.Name(), len(proxies), unique)
	}

	m.logger.InfoBg("Total unique proxies collected: %d", len(allProxies))
	return allProxies, nil
}
