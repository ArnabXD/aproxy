package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"aproxy/internal/logger"
	"aproxy/pkg/manager"
	"aproxy/pkg/scraper"

	netproxy "golang.org/x/net/proxy"
)

type Server struct {
	manager     manager.ProxyManager
	server      *http.Server
	config      *Config
	stats       *Stats
	logger      *logger.Logger
	httpLogger  *logger.Logger
	httpsLogger *logger.Logger
}

type Config struct {
	ListenAddr     string
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	IdleTimeout    time.Duration
	MaxConnections int
	EnableHTTPS    bool
	MaxRetries     int
	StripHeaders   []string
	AddHeaders     map[string]string
}

type Stats struct {
	RequestsHandled   int64
	BytesTransferred  int64
	ActiveConnections int32
	FailedRequests    int64
	mu                sync.RWMutex
}

func NewServer(mgr manager.ProxyManager, config *Config) *Server {
	if config == nil {
		config = DefaultConfig()
	}

	return &Server{
		manager:     mgr,
		config:      config,
		stats:       &Stats{},
		logger:      logger.New("server"),
		httpLogger:  logger.New("http"),
		httpsLogger: logger.New("https"),
	}
}

func DefaultConfig() *Config {
	return &Config{
		ListenAddr:     ":8080",
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxConnections: 1000,
		EnableHTTPS:    true,
		MaxRetries:     3,
		StripHeaders: []string{
			"X-Forwarded-For",
			"X-Real-IP",
			"X-Original-IP",
			"CF-Connecting-IP",
			"True-Client-IP",
		},
		AddHeaders: map[string]string{
			"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
		},
	}
}

func (s *Server) Start() error {
	s.server = &http.Server{
		Addr:           s.config.ListenAddr,
		Handler:        s, // Use the server itself as the handler
		ReadTimeout:    s.config.ReadTimeout,
		WriteTimeout:   s.config.WriteTimeout,
		IdleTimeout:    s.config.IdleTimeout,
		MaxHeaderBytes: 1 << 20,
	}

	return s.server.ListenAndServe()
}

func (s *Server) Stop(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// ServeHTTP implements http.Handler interface
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reqID := logger.GenerateID()

	// Handle special endpoints
	if r.Method == "GET" {
		switch r.URL.Path {
		case "/stats":
			s.handleStats(w, r)
			return
		case "/health":
			s.handleHealth(w, r)
			return
		}
	}

	// Only handle valid proxy requests
	if !s.isValidProxyRequest(r) {
		s.logger.Warn(reqID, "Invalid proxy request: %s %s", r.Method, r.URL.String())
		http.Error(w, "Invalid proxy request", http.StatusBadRequest)
		return
	}

	s.logger.Info(reqID, "Received %s request for %s", r.Method, r.URL.String())

	// Handle proxy requests (both HTTP and HTTPS CONNECT)
	s.handleHTTP(w, r, reqID)
}

// isValidProxyRequest checks if the request is a valid proxy request
func (s *Server) isValidProxyRequest(r *http.Request) bool {
	// CONNECT requests are always valid proxy requests
	if r.Method == http.MethodConnect {
		return true
	}

	// For other methods, the URL must be absolute (have a scheme and host)
	if r.URL.Scheme != "" && r.URL.Host != "" {
		return true
	}

	// Reject relative URLs (like /favicon.ico, /robots.txt, etc.)
	return false
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request, reqID string) {
	if r.Method == http.MethodConnect {
		s.handleHTTPSConnect(w, r, reqID)
		return
	}

	// Retry logic for HTTP requests
	maxRetries := s.config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 1
	}

	s.httpLogger.Debug(reqID, "Starting HTTP proxy attempts (max: %d)", maxRetries)

	for attempt := 0; attempt < maxRetries; attempt++ {
		proxy, err := s.manager.GetNextProxy()
		if err != nil {
			if attempt == maxRetries-1 {
				s.httpLogger.Error(reqID, "No proxies available after %d attempts", maxRetries)
				s.incrementFailedRequests()
				http.Error(w, "No proxy available", http.StatusServiceUnavailable)
				return
			}
			s.httpLogger.Warn(reqID, "No proxy available for attempt %d/%d, retrying", attempt+1, maxRetries)
			continue
		}

		s.httpLogger.Debug(reqID, "Attempt %d/%d using proxy %s", attempt+1, maxRetries, proxy.Address())

		if s.tryProxyHTTPRequest(w, r, proxy, reqID) {
			s.httpLogger.Info(reqID, "Request successful via proxy %s", proxy.Address())
			return // Success
		}

		// Report failure and try next proxy
		s.manager.ReportProxyFailure(*proxy)
		s.httpLogger.Warn(reqID, "Proxy %s failed, trying next", proxy.Address())
	}

	// All attempts failed
	s.httpLogger.Error(reqID, "All %d proxy attempts failed", maxRetries)
	s.incrementFailedRequests()
	http.Error(w, "All proxy attempts failed", http.StatusBadGateway)
}

func (s *Server) handleHTTPSConnect(w http.ResponseWriter, r *http.Request, reqID string) {
	if !s.config.EnableHTTPS {
		s.httpsLogger.Warn(reqID, "HTTPS not enabled in configuration")
		http.Error(w, "HTTPS not supported", http.StatusMethodNotAllowed)
		return
	}

	// Retry logic for HTTPS CONNECT requests
	maxRetries := s.config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 1
	}

	s.httpsLogger.Debug(reqID, "Starting HTTPS CONNECT attempts (max: %d) for %s", maxRetries, r.URL.Host)

	for attempt := 0; attempt < maxRetries; attempt++ {
		proxy, err := s.manager.GetNextProxy()
		if err != nil {
			if attempt == maxRetries-1 {
				s.httpsLogger.Error(reqID, "No proxies available after %d attempts", maxRetries)
				s.incrementFailedRequests()
				http.Error(w, "No proxy available", http.StatusServiceUnavailable)
				return
			}
			s.httpsLogger.Warn(reqID, "No proxy available for attempt %d/%d, retrying", attempt+1, maxRetries)
			continue
		}

		s.httpsLogger.Debug(reqID, "Attempt %d/%d using proxy %s", attempt+1, maxRetries, proxy.Address())

		// Try CONNECT tunnel first, fallback to HTTP proxy method
		if s.tryHTTPSConnect(w, r, proxy, reqID) {
			s.httpsLogger.Info(reqID, "CONNECT tunnel successful via proxy %s", proxy.Address())
			return
		}

		s.httpsLogger.Debug(reqID, "CONNECT tunnel failed, trying HTTP fallback")
		if s.tryHTTPSViaHTTPProxy(w, r, proxy, reqID) {
			s.httpsLogger.Info(reqID, "HTTP fallback successful via proxy %s", proxy.Address())
			return
		}

		// Report failure and try next proxy
		s.manager.ReportProxyFailure(*proxy)
		s.httpsLogger.Warn(reqID, "Proxy %s failed for HTTPS, trying next", proxy.Address())
	}

	// All attempts failed
	s.httpsLogger.Error(reqID, "All %d HTTPS proxy attempts failed", maxRetries)
	s.incrementFailedRequests()
	http.Error(w, "All HTTPS proxy attempts failed", http.StatusBadGateway)
}

func (s *Server) tryProxyHTTPRequest(w http.ResponseWriter, r *http.Request, proxy *scraper.Proxy, reqID string) bool {
	s.httpLogger.Info(reqID, "Using proxy type: %s (%s:%d)", proxy.Type, proxy.Host, proxy.Port)
	s.incrementActiveConnections()
	defer s.decrementActiveConnections()

	var transport *http.Transport

	if proxy.Type == "socks4" || proxy.Type == "socks5" {
		// Use golang.org/x/net/proxy for SOCKS proxies
		proxyAddr := fmt.Sprintf("%s:%d", proxy.Host, proxy.Port)
		dialer, err := netproxy.SOCKS5("tcp", proxyAddr, nil, netproxy.Direct)
		if err != nil {
			s.httpLogger.Error(reqID, "Failed to create SOCKS dialer for %s: %v", proxyAddr, err)
			return false
		}
		transport = &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		}
	} else {
		// HTTP/HTTPS proxy (default)
		proxyURL := fmt.Sprintf("http://%s:%d", proxy.Host, proxy.Port)
		proxyURLParsed, err := url.Parse(proxyURL)
		if err != nil {
			s.httpLogger.Error(reqID, "Invalid proxy URL %s: %v", proxyURL, err)
			return false
		}
		transport = &http.Transport{
			Proxy: http.ProxyURL(proxyURLParsed),
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req := r.Clone(r.Context())
	req.RequestURI = "" // Clear RequestURI for client requests
	s.sanitizeRequest(req)

	resp, err := client.Do(req)
	if err != nil {
		s.httpLogger.Warn(reqID, "HTTP request to %s via proxy %s failed: %v", req.URL.String(), proxy.Address(), err)
		return false
	}
	defer resp.Body.Close()

	s.sanitizeResponse(resp)
	s.copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	written, err := io.Copy(w, resp.Body)
	if err != nil {
		s.httpLogger.Error(reqID, "Error copying response: %v", err)
		return false
	}

	s.incrementRequestsHandled()
	s.addBytesTransferred(written)
	s.httpLogger.Debug(reqID, "HTTP request successful, %d bytes transferred", written)
	return true
}

func (s *Server) tryHTTPSConnect(w http.ResponseWriter, r *http.Request, proxy *scraper.Proxy, reqID string) bool {
	s.httpsLogger.Info(reqID, "Using proxy type: %s (%s:%d)", proxy.Type, proxy.Host, proxy.Port)
	s.incrementActiveConnections()
	defer s.decrementActiveConnections()

	var targetConn net.Conn
	var err error

	if proxy.Type == "socks4" || proxy.Type == "socks5" {
		proxyAddr := fmt.Sprintf("%s:%d", proxy.Host, proxy.Port)
		dialer, errDial := netproxy.SOCKS5("tcp", proxyAddr, nil, netproxy.Direct)
		if errDial != nil {
			s.manager.ReportProxyFailure(*proxy)
			return false
		}
		targetConn, err = dialer.Dial("tcp", r.URL.Host)
	} else {
		targetConn, err = net.DialTimeout("tcp", net.JoinHostPort(proxy.Host, fmt.Sprintf("%d", proxy.Port)), 10*time.Second)
	}
	if err != nil {
		s.manager.ReportProxyFailure(*proxy)
		return false
	}
	defer targetConn.Close()

	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", r.URL.Host, r.URL.Host)
	_, err = targetConn.Write([]byte(connectReq))
	if err != nil {
		s.manager.ReportProxyFailure(*proxy)
		return false
	}

	response, err := http.ReadResponse(bufio.NewReader(targetConn), r)
	if err != nil || response.StatusCode != http.StatusOK {
		s.manager.ReportProxyFailure(*proxy)
		if response != nil {
			s.httpsLogger.Warn(reqID, "CONNECT to %s via proxy %s failed with status %d %s", r.URL.Host, proxy.Address(), response.StatusCode, response.Status)
		} else {
			s.httpsLogger.Warn(reqID, "CONNECT to %s via proxy %s failed with error: %v", r.URL.Host, proxy.Address(), err)
		}
		return false
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		s.manager.ReportProxyFailure(*proxy)
		return false
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		s.manager.ReportProxyFailure(*proxy)
		return false
	}
	defer clientConn.Close()

	clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	// Use channels to coordinate bidirectional copying
	done := make(chan struct{}, 2)

	// Copy client -> target
	go func() {
		defer func() { done <- struct{}{} }()
		io.Copy(targetConn, clientConn)
		targetConn.Close()
	}()

	// Copy target -> client
	go func() {
		defer func() { done <- struct{}{} }()
		written, _ := io.Copy(clientConn, targetConn)
		s.addBytesTransferred(written)
		clientConn.Close()
	}()

	// Wait for both goroutines to complete
	<-done
	<-done

	s.incrementRequestsHandled()
	return true
}

func (s *Server) tryHTTPSViaHTTPProxy(w http.ResponseWriter, r *http.Request, proxy *scraper.Proxy, reqID string) bool {
	s.incrementActiveConnections()
	defer s.decrementActiveConnections()

	// Extract the target host and port from CONNECT request
	host := r.URL.Host
	if !strings.Contains(host, ":") {
		host += ":443" // Default HTTPS port
	}

	s.httpsLogger.Debug(reqID, "Fallback HTTPS via HTTP proxy %s for host %s", proxy.Address(), host)

	// Create a simple tunnel by proxying the raw TCP connection
	proxyAddr := net.JoinHostPort(proxy.Host, fmt.Sprintf("%d", proxy.Port))
	proxyConn, err := net.DialTimeout("tcp", proxyAddr, 10*time.Second)
	if err != nil {
		s.httpsLogger.Warn(reqID, "Failed to connect to proxy %s: %v", proxy.Address(), err)
		return false
	}
	defer proxyConn.Close()

	// Send HTTP CONNECT request to proxy
	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Connection: keep-alive\r\n\r\n", host, host)
	_, err = proxyConn.Write([]byte(connectReq))
	if err != nil {
		s.httpsLogger.Warn(reqID, "Failed to send CONNECT to proxy %s: %v", proxy.Address(), err)
		return false
	}

	// Read the proxy response
	reader := bufio.NewReader(proxyConn)
	resp, err := http.ReadResponse(reader, r)
	if err != nil {
		s.httpsLogger.Warn(reqID, "Error reading proxy response from %s: %v", proxy.Address(), err)
		return false
	}

	// If proxy doesn't support CONNECT, report failure
	if resp.StatusCode != http.StatusOK {
		s.httpsLogger.Warn(reqID, "Proxy %s doesn't support CONNECT (status %d)", proxy.Address(), resp.StatusCode)
		return false
	}

	// Success! Establish the tunnel
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		s.httpsLogger.Error(reqID, "Hijacking not supported")
		return false
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		s.httpsLogger.Error(reqID, "Hijacking failed: %v", err)
		return false
	}
	defer clientConn.Close()

	// Send success response to client
	clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	// Use channels to coordinate bidirectional copying
	done := make(chan struct{}, 2)

	// Copy client -> proxy
	go func() {
		defer func() { done <- struct{}{} }()
		io.Copy(proxyConn, clientConn)
		proxyConn.Close()
	}()

	// Copy proxy -> client
	go func() {
		defer func() { done <- struct{}{} }()
		written, _ := io.Copy(clientConn, proxyConn)
		s.addBytesTransferred(written)
		clientConn.Close()
	}()

	// Wait for both goroutines to complete
	<-done
	<-done

	s.incrementRequestsHandled()
	s.httpsLogger.Debug(reqID, "HTTPS via HTTP proxy %s successful", proxy.Address())
	return true
}

func (s *Server) sanitizeRequest(req *http.Request) {
	for _, header := range s.config.StripHeaders {
		req.Header.Del(header)
	}

	for key, value := range s.config.AddHeaders {
		req.Header.Set(key, value)
	}

	req.Header.Del("Proxy-Connection")
	req.Header.Del("Proxy-Authorization")
}

func (s *Server) sanitizeResponse(resp *http.Response) {
	resp.Header.Del("Server")
	resp.Header.Del("X-Powered-By")
	resp.Header.Del("Via")
}

func (s *Server) copyHeaders(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	managerStats := s.manager.GetStats()
	serverStats := s.getStats()

	// Try to get database stats if this is a DBManager
	dbStatsJSON := `"not_available"`
	if dbManager, ok := s.manager.(*manager.DBManager); ok {
		if dbStats, err := dbManager.GetDBStats(context.Background()); err == nil {
			dbStatsJSON = fmt.Sprintf(`{
				"total_in_db": %d,
				"healthy_in_db": %d,
				"by_type": %s
			}`, dbStats.Total, dbStats.Healthy, formatMap(dbStats.ByType))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{
		"proxy_stats": {
			"cached_proxies": %d,
			"cached_healthy": %d,
			"proxy_types": %s,
			"proxy_countries": %s
		},
		"database_stats": %s,
		"server_stats": {
			"requests_handled": %d,
			"bytes_transferred": %d,
			"active_connections": %d,
			"failed_requests": %d
		}
	}`,
		managerStats.TotalProxies,
		managerStats.HealthyCount,
		formatMap(managerStats.TypeCount),
		formatMap(managerStats.CountryCount),
		dbStatsJSON,
		serverStats.RequestsHandled,
		serverStats.BytesTransferred,
		serverStats.ActiveConnections,
		serverStats.FailedRequests,
	)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	managerStats := s.manager.GetStats()
	if managerStats.HealthyCount == 0 {
		http.Error(w, "No healthy proxies", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK - %d healthy proxies available", managerStats.HealthyCount)
}

func formatMap(m map[string]int) string {
	var parts []string
	for k, v := range m {
		parts = append(parts, fmt.Sprintf(`"%s": %d`, k, v))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func (s *Server) getStats() Stats {
	s.stats.mu.RLock()
	defer s.stats.mu.RUnlock()
	return Stats{
		RequestsHandled:   s.stats.RequestsHandled,
		BytesTransferred:  s.stats.BytesTransferred,
		ActiveConnections: s.stats.ActiveConnections,
		FailedRequests:    s.stats.FailedRequests,
	}
}

func (s *Server) incrementRequestsHandled() {
	s.stats.mu.Lock()
	s.stats.RequestsHandled++
	s.stats.mu.Unlock()
}

func (s *Server) incrementFailedRequests() {
	s.stats.mu.Lock()
	s.stats.FailedRequests++
	s.stats.mu.Unlock()
}

func (s *Server) incrementActiveConnections() {
	s.stats.mu.Lock()
	s.stats.ActiveConnections++
	s.stats.mu.Unlock()
}

func (s *Server) decrementActiveConnections() {
	s.stats.mu.Lock()
	s.stats.ActiveConnections--
	s.stats.mu.Unlock()
}

func (s *Server) addBytesTransferred(bytes int64) {
	s.stats.mu.Lock()
	s.stats.BytesTransferred += bytes
	s.stats.mu.Unlock()
}
