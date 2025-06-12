# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

AProxy is an anonymous proxy server written in Go that scrapes free proxies from various sources, validates their health, and provides a rotating proxy service with privacy features.

## Development Commands

```bash
# Build the application
go build -o aproxy ./cmd/aproxy

# Run the application
./aproxy

# Run with custom config
./aproxy -config config.json

# Generate default config
./aproxy -gen-config

# Run with .env file (copy .env.example to .env first)
cp .env.example .env
./aproxy

# Run tests
go test ./...

# Format code
go fmt ./...

# Run linter (if available)
golangci-lint run

# Tidy dependencies
go mod tidy
```

## Docker Development

```bash
# Build and run with Docker Compose
docker-compose up --build

# Run in background
docker-compose up -d

# View logs
docker-compose logs -f

# Stop containers
docker-compose down

# Build Docker image manually
docker build -t aproxy .

# Run with custom environment
cp .env.docker .env
docker-compose up
```

## Project Architecture

### Core Components

- **Scraper** (`pkg/scraper/`): Fetches proxy lists from multiple sources (ProxyScrape, FreeProxyList)
- **Checker** (`pkg/checker/`): Validates proxy health with SQLite caching and intelligent check intervals
- **Manager** (`pkg/manager/`): Manages proxy pool with database persistence, in-memory cache, and auto-refresh
- **Database** (`internal/database/`): SQLite-based persistent storage with Jet ORM for type-safe queries
- **Proxy Server** (`pkg/proxy/`): HTTP/HTTPS proxy server with privacy features
- **Config** (`internal/config/`): Advanced configuration management with Viper and validation support

### Key Features

- **Non-blocking startup**: Server starts immediately with cached proxies, background checking builds proxy pool progressively
- **Multi-source scraping**: Aggregates proxies from multiple free proxy services
- **SQLite database**: Persistent proxy storage with intelligent caching (10-minute check intervals)
- **Progressive health checking**: Checks proxies in small batches with delays to avoid overwhelming system
- **Smart health monitoring**: Reduces redundant API calls with configurable check intervals
- **Rotating proxy**: Round-robin and random proxy selection with database persistence
- **Privacy protection**: Header stripping, user-agent spoofing, connection sanitization
- **HTTPS support**: CONNECT method tunneling for secure connections
- **Database statistics**: Real-time proxy pool, database, and server metrics via `/stats` endpoint
- **Background operations**: All proxy scraping and checking happens in background without blocking server
- **Performance optimized**: Hybrid in-memory + database storage for fast access
- **Docker support**: Production-ready containerization with persistent volumes
- **Configuration validation**: Comprehensive validation with helpful error messages

### Privacy Features

- Strips identifying headers (X-Forwarded-For, X-Real-IP, etc.)
- Adds spoofed User-Agent headers
- Removes server identification headers from responses
- Supports HTTPS tunneling for encrypted connections

### Configuration

AProxy uses **Viper** for advanced configuration management with validation:

**Configuration Sources (in priority order):**
1. **Command line flags**: `-config`, `-gen-config`, `-version`
2. **Environment variables**: All settings can be overridden with `APROXY_` prefix
3. **Config files**: YAML, JSON, TOML supported (searches `./`, `./config/`, `/etc/aproxy/`)
4. **Defaults**: Sensible defaults for all settings

**Configuration Management:**
```bash
# Generate sample config file
./aproxy -gen-config  # Creates config.yaml

# Run with specific config
./aproxy -config myconfig.yaml

# Use environment variables (Docker-friendly)
export APROXY_SERVER_LISTEN_ADDR=":9090"
export APROXY_DATABASE_PATH="/data/aproxy.db"
./aproxy
```

**Key Features:**
- **Validation**: All config values are validated with helpful error messages
- **Type safety**: Automatic parsing of durations, URLs, file paths
- **Hot reload**: Config file changes detected automatically (if using file watcher)
- **Docker-optimized**: Defaults use `./data/` folder for persistent volumes
- **Environment mapping**: `APROXY_SERVER_LISTEN_ADDR` → `server.listen_addr`

**Configuration Sections:**
- **Server**: Listen address, timeouts, connection limits, protocol support
- **Proxy**: Update intervals, failure thresholds, recheck timing
- **Database**: SQLite path, cleanup intervals, max age settings
- **Checker**: Health check URLs, timeouts, worker pools, intervals, batch checking settings
- **Scraper**: Sources, timeouts, user agents (GitHub sources removed for performance)
- **Logging**: Levels, formats, file rotation settings

**New Checker Configuration Options:**
- `checker.batch_size`: Number of proxies to check in each batch (default: 50)
- `checker.batch_delay`: Delay between batches (default: 30s)
- `checker.background_enabled`: Enable background checking (default: true)

### API Endpoints

- `/`: Main proxy endpoint (HTTP/HTTPS)
- `/stats`: JSON statistics about proxy pool, database, and server metrics
- `/health`: Health check endpoint (returns 200 if healthy proxies available, 503 if none)

### Database Schema

The SQLite database includes:
- **proxies table**: Stores proxy details, health status, and timestamps
- **proxy_checks table**: Historical check data for analytics (optional)
- **Indexes**: Optimized for fast lookups by host:port, status, and timestamps
- **Automatic cleanup**: Removes old unhealthy proxies based on configuration

## Recent Improvements

### Configuration System (v1.1)
- **Migrated to Viper**: Replaced manual config parsing with Viper library
- **Added validation**: All config values validated using `go-playground/validator`
- **YAML support**: Config files now use YAML format (JSON/TOML also supported)
- **Better error messages**: Detailed validation errors with field names and constraints

### Docker Support
- **Production-ready**: Multi-stage Docker build with Alpine Linux
- **Security**: Non-root user, minimal attack surface
- **Health checks**: Proper health endpoint with curl-based Docker healthchecks
- **Persistent volumes**: Database and logs stored in `./data/` for volume mounting
- **Resource limits**: CPU and memory constraints in docker-compose

### Performance Optimizations
- **Non-blocking architecture**: Server starts immediately, proxy checking happens in background
- **Progressive checking**: Proxies checked in small batches with delays to reduce system load
- **Intelligent caching**: Only checks proxies older than 10 minutes, persists all results including failures
- **Fixed race conditions**: HTTPS CONNECT bidirectional copying now uses channel coordination
- **Batch database updates**: Replaced concurrent individual updates with single transaction batches
- **Removed GitHub sources**: Eliminated high-volume proxy sources to reduce database load
- **Improved caching**: Better SQLite connection pooling and prepared statements

### Key Dependencies
- `github.com/spf13/viper`: Configuration management
- `github.com/go-playground/validator/v10`: Configuration validation
- `github.com/go-jet/jet/v2`: Type-safe SQL queries
- `modernc.org/sqlite`: Pure Go SQLite driver

## Development Notes

- Use Go modules for dependency management
- Follow standard Go project layout
- Implement proper error handling and logging
- Use contexts for cancellation and timeouts
- Maintain thread-safe operations with mutexes
- All configuration changes should include validation tags
- Test Docker builds locally before deployment
- Database migrations handled automatically by schema initialization
- Optimize claude for minimal token usage