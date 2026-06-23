package scraper

import (
	"aproxy/internal/logger"
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// source describes a plain-text proxy list: one or more URLs returning lines of
// either "proto://host:port" or "host:port". When a line has no protocol prefix,
// defaultType is used.
type source struct {
	name        string
	urls        []string
	defaultType string // type for bare "host:port" lines
}

// sources is the registry of text-list proxy providers. Add a row to add a source.
var sources = []source{
	{
		name:        "proxyscrape",
		urls:        []string{"https://api.proxyscrape.com/v4/free-proxy-list/get?request=get_proxies&proxy_format=protocolipport&format=text"},
		defaultType: "http",
	},
	{
		name: "freeproxylist",
		urls: []string{
			"https://www.proxy-list.download/api/v1/get?type=http",
			"https://www.proxy-list.download/api/v1/get?type=https",
			"https://www.proxy-list.download/api/v1/get?type=socks4",
			"https://www.proxy-list.download/api/v1/get?type=socks5",
		},
		defaultType: "http", // these lists are bare host:port; type is implied by the URL but we can't see it per-line, so default http. ponytail: type fidelity lost here, acceptable—checker probes the real protocol anyway.
	},
	{
		name:        "github",
		urls:        []string{"https://raw.githubusercontent.com/proxifly/free-proxy-list/refs/heads/main/proxies/all/data.txt"},
		defaultType: "http",
	},
	{
		name: "proxylistorg",
		urls: []string{
			"https://raw.githubusercontent.com/clarketm/proxy-list/master/proxy-list-raw.txt",
			"https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/http.txt",
		},
		defaultType: "http",
	},
}

// listScraper fetches one source's URLs and parses host:port lines.
type listScraper struct {
	src       source
	client    *http.Client
	userAgent string
	logger    *logger.Logger
}

func newListScraper(src source, config ScraperConfig) *listScraper {
	return &listScraper{
		src:       src,
		client:    &http.Client{Timeout: config.Timeout},
		userAgent: config.UserAgent,
		logger:    logger.New(src.name),
	}
}

func (s *listScraper) Name() string { return s.src.name }

func (s *listScraper) Scrape(ctx context.Context) ([]Proxy, error) {
	var all []Proxy
	for _, u := range s.src.urls {
		proxies, err := s.fetch(ctx, u)
		if err != nil {
			s.logger.WarnBg("fetch %s failed: %v", u, err)
			continue
		}
		all = append(all, proxies...)
	}
	s.logger.InfoBg("collected %d proxies", len(all))
	return all, nil
}

func (s *listScraper) fetch(ctx context.Context, url string) ([]Proxy, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.userAgent)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var proxies []Proxy
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if p, ok := parseLine(scanner.Text(), s.src.defaultType); ok {
			proxies = append(proxies, p)
		}
	}
	return proxies, scanner.Err()
}

// parseLine parses "proto://host:port" or "host:port". Returns ok=false for
// blanks, comments, or malformed lines.
func parseLine(line, defaultType string) (Proxy, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return Proxy{}, false
	}

	typ := defaultType
	if scheme, rest, found := strings.Cut(line, "://"); found {
		typ = scheme
		line = rest
	}

	host, portStr, found := strings.Cut(line, ":")
	if !found {
		return Proxy{}, false
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return Proxy{}, false
	}

	return Proxy{Host: host, Port: port, Type: typ, LastSeen: time.Now()}, true
}
