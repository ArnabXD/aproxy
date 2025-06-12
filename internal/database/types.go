package database

import (
	"time"

	"aproxy/pkg/scraper"
)

// ProxyStatus represents the health status of a proxy
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

// CheckResult represents the result of a proxy health check
type CheckResult struct {
	Proxy        scraper.Proxy
	Status       ProxyStatus
	ResponseTime time.Duration
	Error        error
	CheckedAt    time.Time
}
