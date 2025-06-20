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

type ProxyScrapeAPI struct {
	client    *http.Client
	userAgent string
	logger    *logger.Logger
}

func NewProxyScrapeAPI() *ProxyScrapeAPI {
	return &ProxyScrapeAPI{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		logger:    logger.New("proxyscrape"),
	}
}

func NewProxyScrapeAPIWithConfig(config ScraperConfig) *ProxyScrapeAPI {
	return &ProxyScrapeAPI{
		client: &http.Client{
			Timeout: config.Timeout,
		},
		userAgent: config.UserAgent,
		logger:    logger.New("proxyscrape"),
	}
}

func (p *ProxyScrapeAPI) Name() string {
	return "proxyscrape"
}

func (p *ProxyScrapeAPI) Scrape(ctx context.Context) ([]Proxy, error) {
	urls := []string{
		"https://api.proxyscrape.com/v4/free-proxy-list/get?request=get_proxies&proxy_format=protocolipport&format=text",
	}

	var allProxies []Proxy
	for _, apiURL := range urls {
		proxies, err := p.scrapeTextURL(ctx, apiURL)
		if err != nil {
			p.logger.WarnBg("ProxyScrape API failed: %v", err)
			continue
		}

		httpCount := 0
		socksCount := 0
		for _, proxy := range proxies {
			allProxies = append(allProxies, proxy)
			if proxy.Type == "http" || proxy.Type == "https" {
				httpCount++
			} else if proxy.Type == "socks4" || proxy.Type == "socks5" {
				socksCount++
			}
		}
		p.logger.InfoBg("ProxyScrape collected: %d HTTP/HTTPS, %d SOCKS", httpCount, socksCount)
	}

	return allProxies, nil
}

func (p *ProxyScrapeAPI) scrapeTextURL(ctx context.Context, apiURL string) ([]Proxy, error) {
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

	return p.parseProtocolProxies(resp.Body)
}

func (p *ProxyScrapeAPI) parseProtocolProxies(reader io.Reader) ([]Proxy, error) {
	var proxies []Proxy
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Split(line, "://")
		if len(parts) != 2 {
			continue
		}

		protocol := parts[0]
		hostPort := parts[1]

		hostPortParts := strings.Split(hostPort, ":")
		if len(hostPortParts) != 2 {
			continue
		}

		port, err := strconv.Atoi(hostPortParts[1])
		if err != nil {
			continue
		}

		proxy := Proxy{
			Host:     hostPortParts[0],
			Port:     port,
			Type:     protocol,
			LastSeen: time.Now(),
		}

		proxies = append(proxies, proxy)
	}

	return proxies, scanner.Err()
}