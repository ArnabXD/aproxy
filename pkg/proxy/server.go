package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"aproxy/pkg/manager"
	"aproxy/pkg/scraper"
)

type Server struct {
	manager manager.ProxyManager
	server  *http.Server
	config  *Config
	stats   *Stats
	mu      sync.RWMutex
}

type Config struct {
	ListenAddr     string
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	IdleTimeout    time.Duration
	MaxConnections int
	EnableHTTPS    bool
	EnableSOCKS    bool
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
		manager: mgr,
		config:  config,
		stats:   &Stats{},
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
		EnableSOCKS:    false,
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
		http.Error(w, "Invalid proxy request", http.StatusBadRequest)
		return
	}

	// Handle proxy requests (both HTTP and HTTPS CONNECT)
	s.handleHTTP(w, r)
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

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("DEBUG: Received %s request for %s", r.Method, r.URL.String())

	if r.Method == http.MethodConnect {
		s.handleHTTPSConnect(w, r)
		return
	}

	proxy, err := s.manager.GetNextProxy()
	if err != nil {
		s.incrementFailedRequests()
		http.Error(w, "No proxy available", http.StatusServiceUnavailable)
		return
	}

	s.proxyHTTPRequest(w, r, proxy)
}

func (s *Server) handleHTTPSConnect(w http.ResponseWriter, r *http.Request) {
	log.Printf("DEBUG: HTTPS CONNECT request received for %s", r.URL.Host)

	if !s.config.EnableHTTPS {
		http.Error(w, "HTTPS not supported", http.StatusMethodNotAllowed)
		return
	}

	proxy, err := s.manager.GetNextProxy()
	if err != nil {
		s.incrementFailedRequests()
		http.Error(w, "No proxy available", http.StatusServiceUnavailable)
		return
	}

	log.Printf("DEBUG: Using proxy %s for HTTPS CONNECT", proxy.Address())

	// Try CONNECT tunnel first, fallback to HTTP proxy method
	if s.tryHTTPSConnect(w, r, proxy) {
		log.Printf("DEBUG: CONNECT tunnel successful")
		return
	}

	log.Printf("DEBUG: CONNECT failed, trying fallback")
	// Fallback: Convert CONNECT to direct HTTPS request via HTTP proxy
	s.handleHTTPSViaHTTPProxy(w, r, proxy)
}

func (s *Server) proxyHTTPRequest(w http.ResponseWriter, r *http.Request, proxy *scraper.Proxy) {
	s.incrementActiveConnections()
	defer s.decrementActiveConnections()

	proxyURL := fmt.Sprintf("http://%s:%d", proxy.Host, proxy.Port)
	proxyURLParsed, err := url.Parse(proxyURL)
	if err != nil {
		s.manager.ReportProxyFailure(*proxy)
		s.incrementFailedRequests()
		http.Error(w, "Invalid proxy", http.StatusInternalServerError)
		return
	}

	transport := &http.Transport{
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
		s.manager.ReportProxyFailure(*proxy)
		s.incrementFailedRequests()
		log.Printf("DEBUG: HTTP request to %s via proxy %s failed: %v", req.URL.String(), proxy.Address(), err)
		http.Error(w, "Proxy request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	s.sanitizeResponse(resp)
	s.copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	written, err := io.Copy(w, resp.Body)
	if err != nil {
		log.Printf("Error copying response: %v", err)
	}

	s.incrementRequestsHandled()
	s.addBytesTransferred(written)
}

func (s *Server) tryHTTPSConnect(w http.ResponseWriter, r *http.Request, proxy *scraper.Proxy) bool {
	s.incrementActiveConnections()
	defer s.decrementActiveConnections()

	targetConn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", proxy.Host, proxy.Port), 10*time.Second)
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
			log.Printf("DEBUG: CONNECT to %s via proxy %s failed with status %d %s - trying fallback", r.URL.Host, proxy.Address(), response.StatusCode, response.Status)
		} else {
			log.Printf("DEBUG: CONNECT to %s via proxy %s failed with error: %v - trying fallback", r.URL.Host, proxy.Address(), err)
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

func (s *Server) handleHTTPSViaHTTPProxy(w http.ResponseWriter, r *http.Request, proxy *scraper.Proxy) {
	s.incrementActiveConnections()
	defer s.decrementActiveConnections()

	// Extract the target host and port from CONNECT request
	host := r.URL.Host
	if !strings.Contains(host, ":") {
		host += ":443" // Default HTTPS port
	}

	log.Printf("DEBUG: Fallback HTTPS via HTTP proxy %s for host %s", proxy.Address(), host)

	// Create a simple tunnel by proxying the raw TCP connection
	proxyAddr := fmt.Sprintf("%s:%d", proxy.Host, proxy.Port)
	proxyConn, err := net.DialTimeout("tcp", proxyAddr, 10*time.Second)
	if err != nil {
		s.manager.ReportProxyFailure(*proxy)
		s.incrementFailedRequests()
		http.Error(w, "Failed to connect to proxy", http.StatusBadGateway)
		return
	}
	defer proxyConn.Close()

	// Send HTTP CONNECT request to proxy
	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Connection: keep-alive\r\n\r\n", host, host)
	_, err = proxyConn.Write([]byte(connectReq))
	if err != nil {
		s.manager.ReportProxyFailure(*proxy)
		s.incrementFailedRequests()
		http.Error(w, "Failed to send CONNECT", http.StatusBadGateway)
		return
	}

	// Read the proxy response
	reader := bufio.NewReader(proxyConn)
	resp, err := http.ReadResponse(reader, r)
	if err != nil {
		s.manager.ReportProxyFailure(*proxy)
		s.incrementFailedRequests()
		http.Error(w, "Proxy error", http.StatusBadGateway)
		return
	}

	// If proxy doesn't support CONNECT, report failure and try direct connection
	if resp.StatusCode != http.StatusOK {
		s.manager.ReportProxyFailure(*proxy)
		log.Printf("DEBUG: Proxy %s doesn't support CONNECT (%d), attempting direct connection", proxy.Address(), resp.StatusCode)
		s.handleDirectHTTPS(w, r, host)
		return
	}

	// Success! Establish the tunnel
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, "Hijacking failed", http.StatusInternalServerError)
		return
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
}

func (s *Server) handleDirectHTTPS(w http.ResponseWriter, _ *http.Request, host string) {
	// As a last resort, make a direct connection (no proxy)
	log.Printf("DEBUG: Making direct HTTPS connection to %s", host)

	targetConn, err := net.DialTimeout("tcp", host, 10*time.Second)
	if err != nil {
		s.incrementFailedRequests()
		http.Error(w, "Failed to connect directly", http.StatusBadGateway)
		return
	}
	defer targetConn.Close()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, "Hijacking failed", http.StatusInternalServerError)
		return
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
