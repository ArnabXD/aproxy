package checker

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"aproxy/pkg/scraper"
	netproxy "golang.org/x/net/proxy"
)

type ProxyStatus int

const (
	StatusUnknown ProxyStatus = iota
	StatusHealthy
	StatusUnhealthy
	StatusTimeout
	StatusError
)

func (s ProxyStatus) String() string {
	switch s {
	case StatusHealthy:
		return "healthy"
	case StatusUnhealthy:
		return "unhealthy"
	case StatusTimeout:
		return "timeout"
	case StatusError:
		return "error"
	default:
		return "unknown"
	}
}

type CheckResult struct {
	Proxy        scraper.Proxy
	Status       ProxyStatus
	ResponseTime time.Duration
	Error        error
	CheckedAt    time.Time
}

type Checker struct {
	testURL    string
	timeout    time.Duration
	maxWorkers int
	userAgent  string
}

type CheckerConfig struct {
	TestURL    string
	Timeout    time.Duration
	MaxWorkers int
	UserAgent  string
}

func NewChecker() *Checker {
	return &Checker{
		testURL:    "http://httpbin.org/ip",
		timeout:    20 * time.Second,
		maxWorkers: 20,
		userAgent:  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
	}
}

func NewCheckerWithConfig(config CheckerConfig) *Checker {
	return &Checker{
		testURL:    config.TestURL,
		timeout:    config.Timeout,
		maxWorkers: config.MaxWorkers,
		userAgent:  config.UserAgent,
	}
}

func (c *Checker) SetTestURL(url string) {
	c.testURL = url
}

func (c *Checker) SetTimeout(timeout time.Duration) {
	c.timeout = timeout
}

func (c *Checker) SetMaxWorkers(workers int) {
	c.maxWorkers = workers
}

func (c *Checker) CheckProxy(ctx context.Context, proxy scraper.Proxy) CheckResult {
	start := time.Now()
	result := CheckResult{
		Proxy:     proxy,
		CheckedAt: start,
	}

	status, err := c.testProxy(ctx, proxy)
	result.Status = status
	result.Error = err
	result.ResponseTime = time.Since(start)

	return result
}

func (c *Checker) CheckProxies(ctx context.Context, proxies []scraper.Proxy) []CheckResult {
	if len(proxies) == 0 {
		return nil
	}

	workers := c.maxWorkers
	if workers > len(proxies) {
		workers = len(proxies)
	}

	proxyQueue := make(chan scraper.Proxy, len(proxies))
	resultQueue := make(chan CheckResult, len(proxies))

	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for proxy := range proxyQueue {
				select {
				case <-ctx.Done():
					return
				default:
					result := c.CheckProxy(ctx, proxy)
					resultQueue <- result
				}
			}
		}()
	}

	for _, proxy := range proxies {
		proxyQueue <- proxy
	}
	close(proxyQueue)

	go func() {
		wg.Wait()
		close(resultQueue)
	}()

	var results []CheckResult
	healthyCount := 0
	failureCounts := make(map[string]int)

	for result := range resultQueue {
		results = append(results, result)
		if result.Status == StatusHealthy {
			healthyCount++
		} else {
			// Count failures by type
			errType := result.Status.String()
			if result.Error != nil && strings.Contains(result.Error.Error(), "SOCKS proxy not supported") {
				errType = "socks_skipped"
			}
			failureCounts[errType]++

			// Log first few failures for debugging (but not SOCKS)
			if errType != "socks_skipped" && failureCounts[errType] <= 3 {
				fmt.Printf("DEBUG: Proxy %s (%s) failed: %s (error: %v)\n",
					result.Proxy.Address(), result.Proxy.Type, result.Status.String(), result.Error)
			}
		}
	}

	// Summary of results
	if len(results) > 0 {
		fmt.Printf("DEBUG: Results - Healthy: %d", healthyCount)
		for errType, count := range failureCounts {
			fmt.Printf(", %s: %d", errType, count)
		}
		fmt.Printf(" (total: %d)\n", len(results))
	}

	return results
}

func (c *Checker) testProxy(ctx context.Context, proxy scraper.Proxy) (ProxyStatus, error) {
	// Handle SOCKS proxies with specialized testing
	if proxy.Type == "socks4" || proxy.Type == "socks5" {
		return c.testSOCKSProxy(ctx, proxy)
	}

	proxyURL, err := c.buildProxyURL(proxy)
	if err != nil {
		return StatusError, err
	}

	// Create a more permissive transport
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 0, // Disable keep-alive for proxy checks
		}).DialContext,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		DisableKeepAlives:     true, // Important for proxy testing
		DisableCompression:    true,
		MaxIdleConns:          0,
		IdleConnTimeout:       1 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   c.timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects
		},
	}

	// Use a simple, reliable test URL
	req, err := http.NewRequestWithContext(ctx, "GET", c.testURL, nil)
	if err != nil {
		return StatusError, err
	}

	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/plain, application/json")
	req.Header.Set("Connection", "close")

	resp, err := client.Do(req)
	if err != nil {
		if isTimeoutError(err) {
			return StatusTimeout, err
		}
		// Check for common connection errors
		if isConnectionError(err) {
			return StatusUnhealthy, err
		}
		return StatusError, err
	}
	defer resp.Body.Close()

	// Accept any 2xx status code
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return StatusHealthy, nil
	}

	return StatusUnhealthy, fmt.Errorf("HTTP %d", resp.StatusCode)
}

func (c *Checker) buildProxyURL(proxy scraper.Proxy) (*url.URL, error) {
	var scheme string
	switch proxy.Type {
	case "http", "https":
		scheme = "http"
	case "socks4":
		scheme = "socks4"
	case "socks5":
		scheme = "socks5"
	default:
		scheme = "http"
	}

	return url.Parse(fmt.Sprintf("%s://%s:%d", scheme, proxy.Host, proxy.Port))
}

func isTimeoutError(err error) bool {
	if netErr, ok := err.(net.Error); ok {
		return netErr.Timeout()
	}
	return false
}

func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "no route to host") ||
		strings.Contains(errStr, "network is unreachable") ||
		strings.Contains(errStr, "connection reset")
}

func FilterHealthyProxies(results []CheckResult) []scraper.Proxy {
	var healthy []scraper.Proxy
	for _, result := range results {
		if result.Status == StatusHealthy {
			healthy = append(healthy, result.Proxy)
		}
	}
	return healthy
}

func GetHealthyCount(results []CheckResult) int {
	count := 0
	for _, result := range results {
		if result.Status == StatusHealthy {
			count++
		}
	}
	return count
}

func GroupByStatus(results []CheckResult) map[ProxyStatus][]CheckResult {
	groups := make(map[ProxyStatus][]CheckResult)
	for _, result := range results {
		groups[result.Status] = append(groups[result.Status], result)
	}
	return groups
}

// testSOCKSProxy tests SOCKS4 and SOCKS5 proxies
func (c *Checker) testSOCKSProxy(ctx context.Context, proxy scraper.Proxy) (ProxyStatus, error) {
	// For SOCKS proxies, we'll test by establishing a connection and making a simple HTTP request
	// This is more complex than HTTP proxies but necessary for proper validation

	if proxy.Type == "socks4" {
		return c.testSOCKS4Proxy(ctx, proxy)
	} else if proxy.Type == "socks5" {
		return c.testSOCKS5Proxy(ctx, proxy)
	}

	return StatusError, fmt.Errorf("unsupported SOCKS type: %s", proxy.Type)
}

// testSOCKS4Proxy tests a SOCKS4 proxy by making a connection
func (c *Checker) testSOCKS4Proxy(ctx context.Context, proxy scraper.Proxy) (ProxyStatus, error) {
	// Create a dialer that uses the SOCKS4 proxy (note: we'll use SOCKS5 for SOCKS4 as it's more widely supported)
	dialer, err := createSOCKSDialer(proxy.Host, proxy.Port)
	if err != nil {
		return StatusError, err
	}

	// Test by connecting to a simple HTTP endpoint through the SOCKS proxy
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		},
		DisableKeepAlives:     true,
		DisableCompression:    true,
		MaxIdleConns:          0,
		IdleConnTimeout:       1 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   c.timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Make a simple HTTP request through the SOCKS proxy
	req, err := http.NewRequestWithContext(ctx, "GET", c.testURL, nil)
	if err != nil {
		return StatusError, err
	}

	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/plain, application/json")
	req.Header.Set("Connection", "close")

	resp, err := client.Do(req)
	if err != nil {
		if isTimeoutError(err) {
			return StatusTimeout, err
		}
		if isConnectionError(err) {
			return StatusUnhealthy, err
		}
		return StatusError, err
	}
	defer resp.Body.Close()

	// Accept any 2xx status code
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return StatusHealthy, nil
	}

	return StatusUnhealthy, fmt.Errorf("HTTP %d", resp.StatusCode)
}

// testSOCKS5Proxy tests a SOCKS5 proxy by making a connection
func (c *Checker) testSOCKS5Proxy(ctx context.Context, proxy scraper.Proxy) (ProxyStatus, error) {
	// Create a dialer that uses the SOCKS5 proxy
	dialer, err := createSOCKSDialer(proxy.Host, proxy.Port)
	if err != nil {
		return StatusError, err
	}

	// Test by connecting to a simple HTTP endpoint through the SOCKS proxy
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		},
		DisableKeepAlives:     true,
		DisableCompression:    true,
		MaxIdleConns:          0,
		IdleConnTimeout:       1 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   c.timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Make a simple HTTP request through the SOCKS proxy
	req, err := http.NewRequestWithContext(ctx, "GET", c.testURL, nil)
	if err != nil {
		return StatusError, err
	}

	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/plain, application/json")
	req.Header.Set("Connection", "close")

	resp, err := client.Do(req)
	if err != nil {
		if isTimeoutError(err) {
			return StatusTimeout, err
		}
		if isConnectionError(err) {
			return StatusUnhealthy, err
		}
		return StatusError, err
	}
	defer resp.Body.Close()

	// Accept any 2xx status code
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return StatusHealthy, nil
	}

	return StatusUnhealthy, fmt.Errorf("HTTP %d", resp.StatusCode)
}

// createSOCKSDialer creates a dialer that uses a SOCKS5 proxy (works for most SOCKS4 too)
func createSOCKSDialer(host string, port int) (netproxy.Dialer, error) {
	// Using golang.org/x/net/proxy package for SOCKS support
	proxyAddr := fmt.Sprintf("%s:%d", host, port)
	dialer, err := netproxy.SOCKS5("tcp", proxyAddr, nil, netproxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("failed to create SOCKS dialer: %w", err)
	}
	return dialer, nil
}

// TestSingleProxy tests a single proxy manually (for debugging)
func TestSingleProxy(host string, port int) {
	proxy := scraper.Proxy{
		Host: host,
		Port: port,
		Type: "http",
	}

	checker := NewChecker()
	ctx := context.Background()

	fmt.Printf("Testing proxy %s:%d...\n", host, port)
	result := checker.CheckProxy(ctx, proxy)

	fmt.Printf("Result: %s (took %v)\n", result.Status.String(), result.ResponseTime)
	if result.Error != nil {
		fmt.Printf("Error: %v\n", result.Error)
	}
}
