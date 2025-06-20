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

type GitHubProxyScraper struct {
	client    *http.Client
	userAgent string
	logger    *logger.Logger
}

func NewGitHubProxyScraper() *GitHubProxyScraper {
	return &GitHubProxyScraper{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		logger:    logger.New("github"),
	}
}

func NewGitHubProxyScraperWithConfig(config ScraperConfig) *GitHubProxyScraper {
	return &GitHubProxyScraper{
		client: &http.Client{
			Timeout: config.Timeout,
		},
		userAgent: config.UserAgent,
		logger:    logger.New("github"),
	}
}

func (g *GitHubProxyScraper) Name() string {
	return "github"
}

func (g *GitHubProxyScraper) Scrape(ctx context.Context) ([]Proxy, error) {
	url := "https://raw.githubusercontent.com/proxifly/free-proxy-list/refs/heads/main/proxies/all/data.txt"
	
	proxies, err := g.scrapeTextURL(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("GitHub proxy scrape failed: %w", err)
	}

	httpCount := 0
	for _, proxy := range proxies {
		if proxy.Type == "http" || proxy.Type == "https" {
			httpCount++
		}
	}
	
	g.logger.InfoBg("GitHub collected: %d HTTP/HTTPS proxies", httpCount)
	return proxies, nil
}

func (g *GitHubProxyScraper) scrapeTextURL(ctx context.Context, url string) ([]Proxy, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", g.userAgent)

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return g.parseProtocolProxies(resp.Body)
}

func (g *GitHubProxyScraper) parseProtocolProxies(reader io.Reader) ([]Proxy, error) {
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