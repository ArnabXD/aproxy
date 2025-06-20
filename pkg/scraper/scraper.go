package scraper

import (
	"aproxy/internal/logger"
	"context"
)

type MultiScraper struct {
	scrapers []Scraper
	logger   *logger.Logger
}

func NewMultiScraper() *MultiScraper {
	return &MultiScraper{
		scrapers: []Scraper{
			NewProxyScrapeAPI(),
			NewFreeProxyListScraper(),
			NewGeonodeAPIScraper(),
			NewGitHubProxyScraper(),
		},
		logger: logger.New("multiscraper"),
	}
}

func NewMultiScraperWithConfig(config ScraperConfig) *MultiScraper {
	var scrapers []Scraper

	for _, source := range config.Sources {
		switch source {
		case "proxyscrape":
			scrapers = append(scrapers, NewProxyScrapeAPIWithConfig(config))
		case "freeproxylist":
			scrapers = append(scrapers, NewFreeProxyListScraperWithConfig(config))
		case "geonode":
			scrapers = append(scrapers, NewGeonodeAPIScraperWithConfig(config))
		case "proxylistorg":
			scrapers = append(scrapers, NewProxyListOrgScraperWithConfig(config))
		case "github":
			scrapers = append(scrapers, NewGitHubProxyScraperWithConfig(config))
		}
	}

	if len(scrapers) == 0 {
		scrapers = []Scraper{
			NewProxyScrapeAPIWithConfig(config),
			NewFreeProxyListScraperWithConfig(config),
			NewGeonodeAPIScraperWithConfig(config),
			NewGitHubProxyScraperWithConfig(config),
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
	totalUnique := 0

	for _, scraper := range m.scrapers {
		proxies, err := scraper.Scrape(ctx)
		if err != nil {
			m.logger.WarnBg("Scraper %s failed: %v", scraper.Name(), err)
			continue
		}

		uniqueCount := 0
		for _, proxy := range proxies {
			key := proxy.Address()
			if !seen[key] {
				seen[key] = true
				allProxies = append(allProxies, proxy)
				uniqueCount++
			}
		}

		m.logger.InfoBg("Scraper %s: %d total, %d unique", scraper.Name(), len(proxies), uniqueCount)
		totalUnique += uniqueCount
	}

	m.logger.InfoBg("Total unique proxies collected: %d", totalUnique)
	return allProxies, nil
}