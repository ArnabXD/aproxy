package scraper

import (
	"bufio"
	"context"
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

type ProxyScrapeAPI struct {
	client *http.Client
}

func NewProxyScrapeAPI() *ProxyScrapeAPI {
	return &ProxyScrapeAPI{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
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

		// Filter to only HTTP/HTTPS proxies for now
		httpCount := 0
		socksCount := 0
		for _, proxy := range proxies {
			if proxy.Type == "http" || proxy.Type == "https" {
				allProxies = append(allProxies, proxy)
				httpCount++
			} else {
				socksCount++
			}
		}
		log.Printf("ProxyScrape filtered: %d HTTP/HTTPS, %d SOCKS (skipped)", httpCount, socksCount)
	}

	return allProxies, nil
}

func (p *ProxyScrapeAPI) scrapeTextURL(ctx context.Context, apiURL string) ([]Proxy, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

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
	client *http.Client
}

func NewFreeProxyListScraper() *FreeProxyListScraper {
	return &FreeProxyListScraper{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
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

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

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
		},
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
	client *http.Client
}

func NewProxyListOrgScraper() *ProxyListOrgScraper {
	return &ProxyListOrgScraper{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
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

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

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

// GeonodeScraper scrapes from GitHub proxy lists
type GeonodeScraper struct {
	client *http.Client
}

func NewGeonodeScraper() *GeonodeScraper {
	return &GeonodeScraper{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (g *GeonodeScraper) Name() string {
	return "github-proxies"
}

func (g *GeonodeScraper) Scrape(ctx context.Context) ([]Proxy, error) {
	urls := []string{
		"https://raw.githubusercontent.com/proxy4parsing/proxy-list/main/http.txt",
		"https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/http.txt",
		"https://raw.githubusercontent.com/hookzof/socks5_list/master/proxy.txt",
	}

	var allProxies []Proxy
	for _, apiURL := range urls {
		proxies, err := g.scrapeURL(ctx, apiURL)
		if err != nil {
			continue
		}
		allProxies = append(allProxies, proxies...)
	}

	return allProxies, nil
}

func (g *GeonodeScraper) scrapeURL(ctx context.Context, apiURL string) ([]Proxy, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Determine proxy type from URL
	proxyType := "http"
	if strings.Contains(apiURL, "socks5") {
		proxyType = "socks5"
	} else if strings.Contains(apiURL, "socks4") {
		proxyType = "socks4"
	}

	return g.parseProxies(resp.Body, proxyType)
}

func (g *GeonodeScraper) parseProxies(reader io.Reader, proxyType string) ([]Proxy, error) {
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
