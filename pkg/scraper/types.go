package scraper

import (
	"context"
	"fmt"
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
