package scraper

import (
	"aproxy/internal/logger"
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type FreeProxyListScraper struct {
	client    *http.Client
	userAgent string
	logger    *logger.Logger
}

func NewFreeProxyListScraper() *FreeProxyListScraper {
	return &FreeProxyListScraper{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		logger:    logger.New("freeproxylist"),
	}
}

func NewFreeProxyListScraperWithConfig(config ScraperConfig) *FreeProxyListScraper {
	return &FreeProxyListScraper{
		client: &http.Client{
			Timeout: config.Timeout,
		},
		userAgent: config.UserAgent,
		logger:    logger.New("freeproxylist"),
	}
}

func (f *FreeProxyListScraper) Name() string {
	return "freeproxylist"
}

func (f *FreeProxyListScraper) Scrape(ctx context.Context) ([]Proxy, error) {
	urls := []string{
		"https://www.proxy-list.download/api/v1/get?type=http",
		"https://www.proxy-list.download/api/v1/get?type=https",
		"https://www.proxy-list.download/api/v1/get?type=socks4",
		"https://www.proxy-list.download/api/v1/get?type=socks5",
	}

	var allProxies []Proxy
	for _, apiURL := range urls {
		proxies, err := f.scrapeURL(ctx, apiURL)
		if err != nil {
			continue
		}
		allProxies = append(allProxies, proxies...)
	}

	return allProxies, nil
}

func (f *FreeProxyListScraper) scrapeURL(ctx context.Context, apiURL string) ([]Proxy, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", f.userAgent)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return f.parseProxies(resp.Body, getTypeFromURL(apiURL))
}

func (f *FreeProxyListScraper) parseProxies(reader io.Reader, proxyType string) ([]Proxy, error) {
	var proxies []Proxy
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
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

func getTypeFromURL(apiURL string) string {
	u, err := url.Parse(apiURL)
	if err != nil {
		return "http"
	}

	query := u.Query()
	proxyType := query.Get("type")
	if proxyType == "" {
		return "http"
	}
	return proxyType
}