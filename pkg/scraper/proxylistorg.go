package scraper

import (
	"aproxy/internal/logger"
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type ProxyListOrgScraper struct {
	client    *http.Client
	userAgent string
	logger    *logger.Logger
}

func NewProxyListOrgScraper() *ProxyListOrgScraper {
	return &ProxyListOrgScraper{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		logger:    logger.New("proxylistorg"),
	}
}

func NewProxyListOrgScraperWithConfig(config ScraperConfig) *ProxyListOrgScraper {
	return &ProxyListOrgScraper{
		client: &http.Client{
			Timeout: config.Timeout,
		},
		userAgent: config.UserAgent,
		logger:    logger.New("proxylistorg"),
	}
}

func (p *ProxyListOrgScraper) Name() string {
	return "proxylistorg"
}

func (p *ProxyListOrgScraper) Scrape(ctx context.Context) ([]Proxy, error) {
	urls := []string{
		"https://raw.githubusercontent.com/clarketm/proxy-list/master/proxy-list-raw.txt",
		"https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/http.txt",
	}

	var allProxies []Proxy
	for _, apiURL := range urls {
		proxies, err := p.scrapeURL(ctx, apiURL)
		if err != nil {
			continue
		}
		allProxies = append(allProxies, proxies...)
	}

	return allProxies, nil
}

func (p *ProxyListOrgScraper) scrapeURL(ctx context.Context, apiURL string) ([]Proxy, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", p.userAgent)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return p.parseProxies(resp.Body, "http")
}

func (p *ProxyListOrgScraper) parseProxies(reader io.Reader, proxyType string) ([]Proxy, error) {
	var proxies []Proxy
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}

		port, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}

		proxy := Proxy{
			Host:     parts[0],
			Port:     port,
			Type:     proxyType,
			LastSeen: time.Now(),
		}

		proxies = append(proxies, proxy)
	}

	return proxies, scanner.Err()
}