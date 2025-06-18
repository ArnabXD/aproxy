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
- **Authentication**: Bearer token authentication for secure proxy access
- **HTTPS support**: CONNECT method tunneling for secure connections
- **Database statistics**: Real-time proxy pool, database, and server metrics via `/stats` endpoint
- **Background operations**: All proxy scraping and checking happens in background without blocking server
- **Performance optimized**: Hybrid in-memory + database storage for fast access
- **Docker support**: Production-ready containerization with persistent volumes
- **Configuration validation**: Comprehensive validation with helpful error messages

### Authentication & Security

**Authentication (✅ IMPLEMENTED):**
- Bearer token authentication using `server.auth_token` config
- Client authentication via `Proxy-Authorization: Bearer <token>` header
- Environment variable support: `APROXY_SERVER_AUTH_TOKEN`
- Returns 407 Proxy Authentication Required for invalid/missing tokens
- Authentication failures logged with client IP addresses

**Privacy Features:**
- Strips identifying headers (X-Forwarded-For, X-Real-IP, etc.)
- Adds spoofed User-Agent headers
- Removes server identification headers from responses
- Supports HTTPS tunneling for encrypted connections

**Security Considerations:**
- ⚠️ TLS verification disabled (`InsecureSkipVerify: true`) for upstream connections
- ⚠️ SQLite database stored unencrypted at rest
- ⚠️ Authentication tokens stored in plain text
- ⚠️ No built-in rate limiting for authentication attempts
- ⚠️ Limited audit logging for security events

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
export APROXY_SERVER_AUTH_TOKEN="my-secret-token"
./aproxy

# Use authenticated proxy with curl
curl -x http://localhost:8080 \
  -H "Proxy-Authorization: Bearer my-secret-token" \
  http://example.com
```

**Key Features:**
- **Validation**: All config values are validated with helpful error messages
- **Type safety**: Automatic parsing of durations, URLs, file paths
- **Docker-optimized**: Defaults use `./data/` folder for persistent volumes
- **Environment mapping**: `APROXY_SERVER_LISTEN_ADDR` → `server.listen_addr`

**Configuration Sections:**
- **Server**: Listen address, timeouts, connection limits, authentication, header manipulation
- **Proxy**: Update intervals, failure thresholds, recheck timing
- **Database**: SQLite path, cleanup intervals, max age settings
- **Checker**: Health check URLs, timeouts, worker pools, intervals, batch checking settings
- **Scraper**: Sources, timeouts, user agents

**Server Configuration Options:**
- `server.listen_addr`: Server bind address (default: `:8080`)
- `server.enable_https`: Enable HTTPS CONNECT tunneling (default: `true`)
- `server.auth_token`: Optional Bearer token for authentication (default: empty)
- `server.max_connections`: Maximum concurrent connections (default: `1000`)
- `server.strip_headers`: Headers to remove for privacy (X-Forwarded-For, X-Real-IP, etc.)
- `server.add_headers`: Headers to add for spoofing (User-Agent, etc.)
- `server.max_retries`: Maximum retry attempts for failed proxies (default: `3`)

**Checker Configuration Options:**
- `checker.batch_size`: Number of proxies to check in each batch (default: `50`)
- `checker.batch_delay`: Delay between batches (default: `30s`)
- `checker.background_enabled`: Enable background checking (default: `true`)
- `checker.check_interval`: Minimum time between proxy health checks (default: `10m`)
- `checker.timeout`: Proxy test timeout (default: `15s`)
- `checker.max_workers`: Concurrent health check workers (default: `50`)
- `checker.test_url`: URL used to test proxy health (default: `http://icanhazip.com`)

**Logging Configuration:**
- Currently logs to stdout only in JSON format
- Log level can be controlled via command line or environment variables
- File-based logging is not yet implemented

### API Endpoints

- `/`: Main proxy endpoint (HTTP/HTTPS) - requires authentication if configured
- `/stats`: JSON statistics about proxy pool, database, and server metrics - requires authentication if configured
- `/proxies`: JSON list of working proxy servers - requires authentication if configured
- `/health`: Health check endpoint (returns 200 if healthy proxies available, 503 if none) - public, no authentication required

**Authentication:**
All endpoints except `/health` require Bearer token authentication when `server.auth_token` is configured. Use the `Proxy-Authorization: Bearer <token>` header.

```bash
# Access protected endpoints with authentication
curl -H "Proxy-Authorization: Bearer my-secret-token" http://localhost:8080/stats
curl -H "Proxy-Authorization: Bearer my-secret-token" http://localhost:8080/proxies

# Health endpoint is always public
curl http://localhost:8080/health
```

### Database Schema

The SQLite database includes:
- **proxies table**: Stores proxy details, health status, and timestamps
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

## Testing Notes

Currently, the codebase lacks comprehensive test coverage. When adding tests:
- Place unit tests alongside source files with `_test.go` suffix
- Use `go test ./...` to run all tests
- Consider integration tests for proxy health checking and scraping functionality
- Mock external dependencies (proxy sources, HTTP clients) for reliable testing

## Code Patterns and Conventions

- **Interface-based design**: Core components implement interfaces (`ProxyManager`, `Scraper`) for testability
- **Context propagation**: All long-running operations accept and respect `context.Context`
- **Error wrapping**: Use `fmt.Errorf` with `%w` verb to wrap errors with context
- **Structured logging**: Use the internal logger package for consistent logging
- **Configuration validation**: All config structs use validator tags for input validation
- **Database transactions**: Batch operations use single transactions for consistency

## Current Limitations and TODOs

### Not Yet Implemented:
- **SOCKS server support**: AProxy can use SOCKS proxies as upstream but cannot act as a SOCKS server
- **File-based logging**: All logging currently goes to stdout only
- **Configuration hot reload**: Config changes require application restart
- **Comprehensive test coverage**: Limited test suite exists
- **Rate limiting**: No protection against authentication brute force attacks
- **Token rotation**: No automatic authentication token rotation mechanism
- **Database encryption**: SQLite database stored in plain text
- **TLS certificate validation**: Upstream proxy connections skip certificate verification
- **Advanced audit logging**: Limited security event logging and monitoring

### Security Improvements Needed:
- Implement rate limiting for authentication attempts
- Add database encryption at rest
- Provide option for TLS certificate validation
- Add comprehensive audit logging
- Support for token rotation and expiration
- Secure token storage mechanisms