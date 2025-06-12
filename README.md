# AProxy - Anonymous Proxy Server

AProxy is a high-performance anonymous proxy server written in Go that automatically scrapes free proxies from multiple sources, validates their health, and provides a rotating proxy service with advanced privacy features.

## Features

- **Multi-source Scraping**: Aggregates proxies from multiple free proxy services (ProxyScrape, FreeProxyList)
- **SQLite Database**: Persistent proxy storage with intelligent caching to reduce redundant checks
- **Smart Health Monitoring**: Configurable check intervals prevent unnecessary re-validation (default: 10 minutes)
- **Rotating Proxy Pool**: Round-robin and random proxy selection with automatic refresh
- **Privacy Protection**: Header stripping, user-agent spoofing, and connection sanitization
- **HTTPS Support**: CONNECT method tunneling for secure encrypted connections
- **Real-time Statistics**: Live proxy pool, database, and server metrics via REST API
- **Flexible Configuration**: JSON config files and environment variable support
- **Auto-refresh**: Configurable proxy pool updates with health-based filtering
- **Performance Optimized**: Database-backed caching reduces API calls and improves response times

## Quick Start

1. **Clone the repository**
   ```bash
   git clone https://github.com/yourusername/aproxy.git
   cd aproxy
   ```

2. **Build the application**
   ```bash
   go build -o aproxy ./cmd/aproxy
   ```

3. **Run with default settings**
   ```bash
   ./aproxy
   ```

4. **Use the proxy**
   ```bash
   # Test with curl
   curl -x http://localhost:8080 http://httpbin.org/ip
   
   # Use with any HTTP client
   export HTTP_PROXY=http://localhost:8080
   export HTTPS_PROXY=http://localhost:8080
   ```

## Configuration

AProxy supports multiple configuration methods:

### 1. Environment Variables (.env file)

Copy `.env.example` to `.env` and modify as needed:

```bash
cp .env.example .env
```

Key environment variables:

**Server Configuration:**
- `APROXY_LISTEN_ADDR`: Server address (default: `:8080`)
- `APROXY_ENABLE_HTTPS`: Enable HTTPS CONNECT support (default: `true`)
- `APROXY_ENABLE_SOCKS`: Enable SOCKS proxy support (default: `false`)

**Proxy Management:**
- `APROXY_UPDATE_INTERVAL`: Proxy refresh interval (default: `15m`)
- `APROXY_MAX_FAILURES`: Max failures before removing proxy (default: `3`)
- `APROXY_RECHECK_TIME`: Time before re-checking failed proxies (default: `5m`)

**Health Checking:**
- `APROXY_CHECKER_CHECK_INTERVAL`: Minimum time between proxy health checks (default: `10m`)
- `APROXY_CHECKER_TIMEOUT`: Proxy test timeout (default: `15s`)
- `APROXY_CHECKER_MAX_WORKERS`: Concurrent health check workers (default: `50`)
- `APROXY_CHECKER_TEST_URL`: URL used to test proxy health (default: `http://icanhazip.com`)

**Database Configuration:**
- `APROXY_DATABASE_PATH`: SQLite database file path (default: `./aproxy.db`)
- `APROXY_DATABASE_MAX_AGE`: Maximum age for proxy records before cleanup (default: `24h`)
- `APROXY_DATABASE_CLEANUP_INTERVAL`: How often to run database cleanup (default: `1h`)

### 2. JSON Configuration File

Generate a default config file:
```bash
./aproxy -gen-config
```

Then run with custom config:
```bash
./aproxy -config config.json
```

### 3. Command Line Options

```bash
# Show version
./aproxy -version

# Generate default config
./aproxy -gen-config

# Run with custom config file
./aproxy -config /path/to/config.json
```

## Database Features

AProxy uses SQLite for intelligent proxy caching and persistent storage:

### Smart Caching
- **Check Intervals**: Proxies are only re-checked after the configured interval (default: 10 minutes)
- **Persistent Health**: Proxy health status survives application restarts
- **Reduced API Calls**: Significantly fewer requests to proxy validation endpoints
- **Automatic Cleanup**: Old unhealthy proxies are automatically removed

### Database Schema
The SQLite database stores:
- Proxy details (host, port, type, country)
- Health status and response times
- Check timestamps and failure counts
- Indexing for fast lookups and filtering

### Performance Benefits
- **Fast Startup**: Loads existing healthy proxies immediately
- **Reduced Latency**: In-memory cache with database fallback
- **Bandwidth Savings**: Fewer redundant health checks
- **Historical Data**: Track proxy performance over time

Example database statistics:
```bash
# View database stats via API
curl http://localhost:8080/stats | jq '.database_stats'
```

## Privacy Features

AProxy includes comprehensive privacy protection:

### Header Sanitization
- Strips identifying headers: `X-Forwarded-For`, `X-Real-IP`, `X-Original-IP`, `CF-Connecting-IP`, `True-Client-IP`
- Removes server identification headers from responses
- Adds spoofed `User-Agent` headers

### Connection Privacy
- Removes `Proxy-Connection` and `Proxy-Authorization` headers
- Sanitizes server response headers (`Server`, `X-Powered-By`, `Via`)
- Supports HTTPS tunneling for end-to-end encryption

## API Endpoints

### Main Proxy Endpoint
- **URL**: `http://localhost:8080/`
- **Methods**: `GET`, `POST`, `PUT`, `DELETE`, `CONNECT`
- **Description**: Main proxy endpoint for HTTP/HTTPS requests

### Statistics Endpoint
- **URL**: `http://localhost:8080/stats`
- **Method**: `GET`
- **Response**: JSON with proxy pool and server statistics

```json
{
  "proxy_stats": {
    "total_proxies": 150,
    "healthy_proxies": 120,
    "proxy_types": {"http": 80, "https": 40},
    "proxy_countries": {"US": 60, "GB": 30, "DE": 30}
  },
  "database_stats": {
    "total_stored": 500,
    "healthy_stored": 150,
    "by_type": {"http": 300, "https": 120, "socks5": 80}
  },
  "server_stats": {
    "requests_handled": 1234,
    "bytes_transferred": 5678901,
    "active_connections": 12,
    "failed_requests": 56
  }
}
```

### Health Check Endpoint
- **URL**: `http://localhost:8080/health`
- **Method**: `GET`
- **Response**: Server health status and proxy availability

## Architecture

AProxy follows a modular architecture with clear separation of concerns:

### Core Components

- **Scraper** (`pkg/scraper/`): Multi-source proxy scraping with deduplication
- **Checker** (`pkg/checker/`): Concurrent proxy health validation with SQLite caching
- **Manager** (`pkg/manager/`): Proxy pool management with rotation and health tracking
- **Database** (`internal/database/`): SQLite-based persistent proxy storage and caching
- **Proxy Server** (`pkg/proxy/`): HTTP/HTTPS proxy server with privacy features
- **Config** (`internal/config/`): Configuration management with JSON and env support

### Data Flow

1. **Scraper** fetches proxies from multiple sources
2. **Database** stores proxies with timestamps and health status
3. **Checker** validates proxy health with intelligent caching (skips recent checks)
4. **Manager** maintains healthy proxy pool with database persistence and in-memory cache
5. **Proxy Server** handles client requests using rotating proxies
6. **Health monitoring** continuously validates and refreshes proxy pool
7. **Cleanup** automatically removes old unhealthy proxies from database

## Development

### Prerequisites
- Go 1.19 or later
- Internet connection for proxy scraping

### Build Commands
```bash
# Build the application
go build -o aproxy ./cmd/aproxy

# Run tests
go test ./...

# Format code
go fmt ./...

# Tidy dependencies
go mod tidy
```

### Project Structure
```
aproxy/
├── cmd/aproxy/          # Main application
├── pkg/
│   ├── scraper/         # Proxy scraping logic
│   ├── checker/         # Proxy health checking (with SQLite caching)
│   ├── manager/         # Proxy pool management (database + memory)
│   └── proxy/           # HTTP proxy server
├── internal/
│   ├── config/          # Configuration management
│   └── database/        # SQLite database layer
│       ├── models/      # Generated database models
│       ├── schema.sql   # Database schema
│       ├── db.go        # Database initialization
│       ├── service.go   # Database operations
│       └── types.go     # Database types
├── .env.example         # Environment variable template
├── CLAUDE.md           # AI assistant instructions
└── README.md           # This file
```

## Proxy Sources

AProxy currently supports the following proxy sources:

- **ProxyScrape API**: Free proxy list with filtering options
- **FreeProxyList**: Another reliable source for free proxies

Additional sources can be easily added by implementing the `Scraper` interface.

## Performance

AProxy is designed for high performance:

- **SQLite Caching**: Intelligent proxy health caching reduces redundant API calls
- **Configurable Check Intervals**: Prevents unnecessary re-validation (default: 10 minutes)
- **Concurrent Processing**: Parallel proxy checking with configurable worker pools
- **Connection Pooling**: Efficient HTTP transport with connection reuse
- **Memory + Database**: Hybrid storage with fast in-memory cache and persistent SQLite
- **Automatic Cleanup**: Background removal of old unhealthy proxies
- **Scalable**: Handles thousands of concurrent connections with minimal overhead

## Monitoring

Monitor AProxy performance using:

1. **Built-in Stats**: `/stats` endpoint provides real-time metrics
2. **Health Checks**: `/health` endpoint for uptime monitoring
3. **Logs**: Structured JSON logging with configurable levels
4. **Proxy Pool Status**: Real-time proxy health and failure tracking

## Security Considerations

- AProxy strips identifying headers but doesn't encrypt traffic itself
- Use HTTPS endpoints for sensitive data
- Regularly monitor proxy sources for reliability
- Consider rate limiting for production deployments
- Free proxies may have limited reliability and privacy guarantees

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests for new functionality
5. Submit a pull request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Disclaimer

This software is provided for educational and research purposes. Users are responsible for ensuring compliance with applicable laws and regulations. The authors are not responsible for any misuse of this software.

## Support

For issues, questions, or contributions:
- Open an issue on GitHub
- Check existing documentation
- Review the source code for implementation details