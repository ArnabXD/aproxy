# AProxy - Anonymous Proxy Server

AProxy is a high-performance anonymous proxy server written in Go that automatically scrapes free proxies from multiple sources, validates their health, and provides a rotating proxy service with advanced privacy features.

## Features

- **Multi-source Scraping**: Aggregates proxies from multiple free proxy services (ProxyScrape, FreeProxyList)
- **SQLite Database**: Persistent proxy storage with intelligent caching to reduce redundant checks
- **Smart Health Monitoring**: Configurable check intervals prevent unnecessary re-validation (default: 10 minutes)
- **Rotating Proxy Pool**: Round-robin and random proxy selection with automatic refresh
- **Privacy Protection**: Header stripping, user-agent spoofing, and connection sanitization
- **HTTPS Support**: CONNECT method tunneling for secure encrypted connections
- **Authentication Support**: Optional token-based authentication to prevent unauthorized usage
- **SOCKS Proxy Support**: Full support for SOCKS4 and SOCKS5 proxies with auto-detection
- **Real-time Statistics**: Live proxy pool, database, and server metrics via REST API
- **Flexible Configuration**: YAML config files and environment variable support
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
   # Test with curl (no auth)
   curl -x http://localhost:8080 http://httpbin.org/ip
   
   # Test with authentication (if auth_token is configured)
   curl -x http://localhost:8080 --proxy-header "Proxy-Authorization: Bearer your-token" http://httpbin.org/ip
   
   # Use with any HTTP client
   export HTTP_PROXY=http://localhost:8080
   export HTTPS_PROXY=http://localhost:8080
   ```

## Configuration

AProxy supports multiple configuration methods:

### 1. YAML Configuration File (Recommended)

Generate a default config file:
```bash
./aproxy -gen-config  # Creates config.yaml
```

Then run with the config:
```bash
./aproxy -config config.yaml
```

### 2. Environment Variables (Docker-friendly)

All configuration options can be set via environment variables using the `APROXY_` prefix:

**Server Configuration:**
- `APROXY_SERVER_LISTEN_ADDR`: Server bind address (default: `:8080`)
- `APROXY_SERVER_ENABLE_HTTPS`: Enable HTTPS CONNECT support (default: `true`)
- `APROXY_SERVER_MAX_CONNECTIONS`: Maximum concurrent connections (default: `1000`)
- `APROXY_SERVER_MAX_RETRIES`: Maximum retry attempts for failed proxies (default: `3`)
- `APROXY_SERVER_AUTH_TOKEN`: Optional authentication token to prevent unauthorized usage

**Proxy Management:**
- `APROXY_PROXY_UPDATE_INTERVAL`: Proxy refresh interval (default: `15m`)
- `APROXY_PROXY_MAX_FAILURES`: Max failures before removing proxy (default: `3`)
- `APROXY_PROXY_RECHECK_TIME`: Time before re-checking failed proxies (default: `5m`)

**Health Checking:**
- `APROXY_CHECKER_CHECK_INTERVAL`: Minimum time between proxy health checks (default: `10m`)
- `APROXY_CHECKER_TIMEOUT`: Proxy test timeout (default: `15s`)
- `APROXY_CHECKER_MAX_WORKERS`: Concurrent health check workers (default: `50`)
- `APROXY_CHECKER_TEST_URL`: URL used to test proxy health (default: `http://icanhazip.com`)
- `APROXY_CHECKER_BATCH_SIZE`: Number of proxies to check in each batch (default: `50`)
- `APROXY_CHECKER_BATCH_DELAY`: Delay between batches (default: `30s`)
- `APROXY_CHECKER_BACKGROUND_ENABLED`: Enable background checking (default: `true`)

**Database Configuration:**
- `APROXY_DATABASE_PATH`: SQLite database file path (default: `./data/aproxy.db`)
- `APROXY_DATABASE_MAX_AGE`: Maximum age for proxy records before cleanup (default: `24h`)
- `APROXY_DATABASE_CLEANUP_INTERVAL`: How often to run database cleanup (default: `1h`)

**Scraper Configuration:**
- `APROXY_SCRAPER_TIMEOUT`: Scraper request timeout (default: `30s`)
- `APROXY_SCRAPER_SOURCES`: Comma-separated list of proxy sources (default: `proxyscrape,freeproxylist`)


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

## Authentication

AProxy supports optional token-based authentication to prevent unauthorized usage:

### Configuration

**Via YAML config:**
```yaml
server:
  auth_token: "your-secret-token-here"
```

**Via environment variable:**
```bash
export APROXY_SERVER_AUTH_TOKEN="your-secret-token-here"
```

**Via Docker:**
```bash
# In .env file
APROXY_SERVER_AUTH_TOKEN=my-secret-token

# Or as Docker run argument
docker run -e APROXY_SERVER_AUTH_TOKEN=my-secret-token aproxy
```

### Client Usage

When authentication is enabled, clients must include the `Proxy-Authorization` header:

```bash
# curl with authentication
curl -x http://localhost:8080 \
     --proxy-header "Proxy-Authorization: Bearer your-secret-token-here" \
     http://httpbin.org/ip

# Python requests example
import requests

proxies = {
    'http': 'http://localhost:8080',
    'https': 'http://localhost:8080'
}

headers = {
    'Proxy-Authorization': 'Bearer your-secret-token-here'
}

response = requests.get('http://httpbin.org/ip', proxies=proxies, headers=headers)
```

### Security Notes
- Authentication is completely optional - if no `auth_token` is configured, the proxy works without authentication
- Tokens are validated using Bearer token format: `Proxy-Authorization: Bearer <token>`
- Unauthorized requests receive HTTP 407 Proxy Authentication Required response
- Use strong, random tokens for production deployments

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
- Go 1.24 or later
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

## Docker Deployment

### Using Docker Compose (Recommended)

```bash
# Build and run with Docker Compose
docker-compose up --build

# Run in background
docker-compose up -d --build

# View logs
docker-compose logs -f

# Stop containers
docker-compose down

# Clean up volumes (removes database)
docker-compose down -v
```

### Manual Docker Build

```bash
# Build Docker image
docker build -t aproxy .

# Run with persistent data volume
docker run -d \
  --name aproxy \
  -p 8080:8080 \
  -v aproxy-data:/app/data \
  aproxy

# View logs
docker logs -f aproxy
```

### Docker Features

- **Persistent Storage**: Database and logs stored in `./data/` volume
- **Health Checks**: Built-in health monitoring via `/health` endpoint
- **Resource Limits**: CPU and memory constraints configured
- **Non-root User**: Runs as unprivileged user for security
- **Alpine Linux**: Minimal attack surface with small image size

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
│   ├── config/          # Configuration management (Viper-based)
│   ├── database/        # SQLite database layer with Jet ORM
│   └── logger/          # Structured logging
├── data/                # Persistent data directory
│   ├── aproxy.db        # SQLite database
│   └── aproxy.log       # Application logs
├── config.example.yaml  # Example YAML configuration
├── config.yaml          # Active YAML configuration
├── docker-compose.yml   # Docker Compose configuration
├── Dockerfile           # Docker build configuration
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
3. **Proxy Pool Status**: Real-time proxy health and failure tracking

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