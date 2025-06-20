package scraper

import (
	"aproxy/internal/logger"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type GeonodeAPIScraper struct {
	client    *http.Client
	userAgent string
	logger    *logger.Logger
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
		logger:    logger.New("geonode"),
	}
}

func NewGeonodeAPIScraperWithConfig(config ScraperConfig) *GeonodeAPIScraper {
	return &GeonodeAPIScraper{
		client: &http.Client{
			Timeout: config.Timeout,
		},
		userAgent: config.UserAgent,
		logger:    logger.New("geonode"),
	}
}

func (g *GeonodeAPIScraper) Name() string {
	return "geonode-api"
}

func (g *GeonodeAPIScraper) Scrape(ctx context.Context) ([]Proxy, error) {
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
			continue
		}

		for _, protocol := range geoProxy.Protocols {
			proxy := Proxy{
				Host:     geoProxy.IP,
				Port:     port,
				Type:     protocol,
				Country:  geoProxy.Country,
				LastSeen: time.Now(),
			}

			proxies = append(proxies, proxy)

			if protocol == "http" || protocol == "https" {
				httpCount++
			} else if protocol == "socks4" || protocol == "socks5" {
				socksCount++
			}
		}
	}

	g.logger.InfoBg("Geonode API collected: %d HTTP/HTTPS, %d SOCKS from %d total proxies",
		httpCount, socksCount, len(geonodeResp.Data))

	return proxies, nil
}