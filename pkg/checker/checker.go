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

	"aproxy/internal/config"
	"aproxy/internal/logger"
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
	logger     *logger.Logger
}

func NewChecker(config config.CheckerConfig) *Checker {
	return &Checker{
		testURL:    config.TestURL,
		timeout:    config.Timeout,
		maxWorkers: config.MaxWorkers,
		userAgent:  config.UserAgent,
		logger:     logger.New("checker"),
	}
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

	for result := range resultQueue {
		results = append(results, result)
		if result.Status == StatusHealthy {
			healthyCount++
		}
	}

	if len(results) > 0 {
		c.logger.InfoBg("Proxy check completed: %d healthy out of %d total", healthyCount, len(results))
	}

	return results
}

func (c *Checker) testProxy(ctx context.Context, proxy scraper.Proxy) (ProxyStatus, error) {
	transport, err := c.buildTransport(proxy)
	if err != nil {
		return StatusError, err
	}
	return c.runCheck(ctx, transport)
}

// buildTransport returns an http.Transport routed through the given proxy,
// for HTTP/HTTPS proxies (via Proxy URL) or SOCKS proxies (via a SOCKS dialer).
func (c *Checker) buildTransport(proxy scraper.Proxy) (*http.Transport, error) {
	transport := &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		DisableKeepAlives:     true,
		DisableCompression:    true,
		MaxIdleConns:          0,
		IdleConnTimeout:       1 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	if proxy.Type == "socks4" || proxy.Type == "socks5" {
		// ponytail: x/net/proxy has no SOCKS4 dialer, so socks4 is probed via
		// the SOCKS5 handshake. A pure-SOCKS4 proxy will fail this and be marked
		// unhealthy — acceptable; swap in a socks4 dialer if you need them.
		dialer, err := createSOCKSDialer(proxy.Host, proxy.Port)
		if err != nil {
			return nil, err
		}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
		return transport, nil
	}

	// HTTP/HTTPS proxy.
	proxyURL, err := url.Parse(fmt.Sprintf("http://%s:%d", proxy.Host, proxy.Port))
	if err != nil {
		return nil, err
	}
	transport.Proxy = http.ProxyURL(proxyURL)
	transport.DialContext = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 0}).DialContext
	return transport, nil
}

// runCheck issues the test request over the transport and classifies the result.
func (c *Checker) runCheck(ctx context.Context, transport *http.Transport) (ProxyStatus, error) {
	client := &http.Client{
		Transport: transport,
		Timeout:   c.timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

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

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return StatusHealthy, nil
	}
	return StatusUnhealthy, fmt.Errorf("HTTP %d", resp.StatusCode)
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
