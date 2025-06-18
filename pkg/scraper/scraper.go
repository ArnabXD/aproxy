package scraper

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Proxy struct {
	Host     string
	Port     int
	Type     string
	Country  string
	LastSeen time.Time
}

func (p Proxy) Address() string {
	return fmt.Sprintf("%s:%d", p.Host, p.Port)
}

type Scraper interface {
	Name() string
	Scrape(ctx context.Context) ([]Proxy, error)
}

type ScraperConfig struct {
	Timeout   time.Duration
	UserAgent string
	Sources   []string
}

type ProxyScrapeAPI struct {
	client    *http.Client
	userAgent string
}

func NewProxyScrapeAPI() *ProxyScrapeAPI {
	return &ProxyScrapeAPI{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
	}
}

func NewProxyScrapeAPIWithConfig(config ScraperConfig) *ProxyScrapeAPI {
	return &ProxyScrapeAPI{
		client: &http.Client{
			Timeout: config.Timeout,
		},
		userAgent: config.UserAgent,
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
			log.Printf("ProxyScrape API failed: %v", err)
			continue
		}

		// Include all proxy types - HTTP, HTTPS, and SOCKS
		httpCount := 0
		socksCount := 0
		for _, proxy := range proxies {
			allProxies = append(allProxies, proxy) // Include all proxies
			if proxy.Type == "http" || proxy.Type == "https" {
				httpCount++
			} else if proxy.Type == "socks4" || proxy.Type == "socks5" {
				socksCount++
			}
		}
		log.Printf("ProxyScrape collected: %d HTTP/HTTPS, %d SOCKS", httpCount, socksCount)
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

		// Parse format: protocol://host:port
		// Examples: socks4://184.181.217.210:4145, http://119.3.113.152:9094
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

type FreeProxyListScraper struct {
	client    *http.Client
	userAgent string
}

func NewFreeProxyListScraper() *FreeProxyListScraper {
	return &FreeProxyListScraper{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
	}
}

func NewFreeProxyListScraperWithConfig(config ScraperConfig) *FreeProxyListScraper {
	return &FreeProxyListScraper{
		client: &http.Client{
			Timeout: config.Timeout,
		},
		userAgent: config.UserAgent,
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

type MultiScraper struct {
	scrapers []Scraper
}

func NewMultiScraper() *MultiScraper {
	return &MultiScraper{
		scrapers: []Scraper{
			NewProxyScrapeAPI(),
			NewFreeProxyListScraper(),
			NewGeonodeAPIScraper(),
		},
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
		}
	}

	if len(scrapers) == 0 {
		// Default scrapers if none specified
		scrapers = []Scraper{
			NewProxyScrapeAPIWithConfig(config),
			NewFreeProxyListScraperWithConfig(config),
			NewGeonodeAPIScraperWithConfig(config),
		}
	}

	return &MultiScraper{
		scrapers: scrapers,
	}
}

func (m *MultiScraper) ScrapeAll(ctx context.Context) ([]Proxy, error) {
	var allProxies []Proxy
	seen := make(map[string]bool)
	totalUnique := 0

	for _, scraper := range m.scrapers {
		proxies, err := scraper.Scrape(ctx)
		if err != nil {
			log.Printf("Scraper %s failed: %v", scraper.Name(), err)
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

		log.Printf("Scraper %s: %d total, %d unique", scraper.Name(), len(proxies), uniqueCount)
		totalUnique += uniqueCount
	}

	log.Printf("Total unique proxies collected: %d", totalUnique)
	return allProxies, nil
}

// ProxyListOrgScraper scrapes from proxy-list.org
type ProxyListOrgScraper struct {
	client    *http.Client
	userAgent string
}

func NewProxyListOrgScraper() *ProxyListOrgScraper {
	return &ProxyListOrgScraper{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
	}
}

func NewProxyListOrgScraperWithConfig(config ScraperConfig) *ProxyListOrgScraper {
	return &ProxyListOrgScraper{
		client: &http.Client{
			Timeout: config.Timeout,
		},
		userAgent: config.UserAgent,
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

// GeonodeAPIScraper scrapes from Geonode free proxy API
type GeonodeAPIScraper struct {
	client    *http.Client
	userAgent string
}

type GeonodeResponse struct {
	Data  []GeonodeProxy `json:"data"`
	Total int            `json:"total"`
	Page  int            `json:"page"`
	Limit int            `json:"limit"`
}

type GeonodeProxy struct {
	ID             string   `json:"_id"`
	IP             string   `json:"ip"`
	Port           string   `json:"port"`
	Protocols      []string `json:"protocols"`
	Country        string   `json:"country"`
	AnonymityLevel string   `json:"anonymityLevel"`
	Latency        float64  `json:"latency"`
	Speed          int      `json:"speed"`
	UpTime         float64  `json:"upTime"`
	LastChecked    int64    `json:"lastChecked"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

func NewGeonodeAPIScraper() *GeonodeAPIScraper {
	return &GeonodeAPIScraper{
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
	}
}

func NewGeonodeAPIScraperWithConfig(config ScraperConfig) *GeonodeAPIScraper {
	return &GeonodeAPIScraper{
		client: &http.Client{
			Timeout: config.Timeout,
		},
		userAgent: config.UserAgent,
	}
}

func (g *GeonodeAPIScraper) Name() string {
	return "geonode-api"
}

func (g *GeonodeAPIScraper) Scrape(ctx context.Context) ([]Proxy, error) {
	// Use limit=1000 as requested
	apiURL := "https://proxylist.geonode.com/api/proxy-list?limit=500"

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", g.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var geonodeResp GeonodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&geonodeResp); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	var proxies []Proxy
	httpCount := 0
	socksCount := 0

	for _, geoProxy := range geonodeResp.Data {
		port, err := strconv.Atoi(geoProxy.Port)
		if err != nil {
			continue // Skip invalid ports
		}

		// Process each protocol supported by the proxy
		for _, protocol := range geoProxy.Protocols {
			proxy := Proxy{
				Host:     geoProxy.IP,
				Port:     port,
				Type:     protocol,
				Country:  geoProxy.Country,
				LastSeen: time.Now(),
			}

			proxies = append(proxies, proxy)

			// Count proxy types
			if protocol == "http" || protocol == "https" {
				httpCount++
			} else if protocol == "socks4" || protocol == "socks5" {
				socksCount++
			}
		}
	}

	log.Printf("Geonode API collected: %d HTTP/HTTPS, %d SOCKS from %d total proxies",
		httpCount, socksCount, len(geonodeResp.Data))

	return proxies, nil
}
