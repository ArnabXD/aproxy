# AProxy - Anonymous Proxy Server

A high-performance proxy server that automatically scrapes, validates, and rotates free proxies. Built for penetration testing, privacy research, and anonymous browsing.

## What is AProxy?

AProxy aggregates free proxies from multiple sources, validates their health, and provides a rotating HTTP/HTTPS proxy service. It's designed for security researchers, penetration testers, and anyone needing anonymous internet access.

**Key Features:**
- 🔄 **Auto-rotating proxy pool** - Automatically cycles through working proxies
- 🗄️ **Persistent caching** - SQLite database remembers proxy health to reduce checks
- 🔒 **Privacy protection** - Strips identifying headers and spoofs user agents
- ⚡ **High performance** - Concurrent health checking with intelligent batching
- 🛡️ **Optional authentication** - Bearer token protection to prevent unauthorized usage
- 📊 **Real-time monitoring** - REST API for stats and proxy list management

## Quick Start

```bash
# Clone and build
git clone https://github.com/ArnabXD/aproxy.git
cd aproxy
go build -o aproxy ./cmd/aproxy

# Run with defaults (starts on :8080)
./aproxy

# Test it works
curl -x http://localhost:8080 http://httpbin.org/ip
```

## Configuration

### Environment Variables (Recommended)
```bash
# Server settings
export APROXY_SERVER_LISTEN_ADDR=":8080"
export APROXY_SERVER_AUTH_TOKEN="your-secret-token"  # Optional authentication

# Proxy management
export APROXY_PROXY_UPDATE_INTERVAL="15m"           # How often to refresh proxy pool
export APROXY_CHECKER_CHECK_INTERVAL="10m"          # Min time between health checks
export APROXY_DATABASE_PATH="./data/aproxy.db"      # Where to store proxy cache

# Start with config
./aproxy
```

### YAML Config File
```bash
# Generate default config
./aproxy -gen-config

# Run with config file
./aproxy -config config.yaml
```

## Authentication

Protect your proxy server with token authentication:

```bash
# Set auth token
export APROXY_SERVER_AUTH_TOKEN="my-secret-token"
./aproxy

# Use authenticated proxy
curl -x http://localhost:8080 \
  -H "Proxy-Authorization: Bearer my-secret-token" \
  http://example.com
```

## API Endpoints

| Endpoint | Auth Required | Description |
|----------|---------------|-------------|
| `/` | Yes | Main proxy endpoint (HTTP/HTTPS) |
| `/stats` | Yes | JSON statistics about proxy pool and server |
| `/proxies` | Yes | List of all working proxy servers |
| `/health` | No | Health check (200 if proxies available, 503 if none) |

### Usage Examples

```bash
# Check proxy health (public endpoint)
curl http://localhost:8080/health

# Get proxy statistics (requires auth)
curl -H "Proxy-Authorization: Bearer token" http://localhost:8080/stats

# List all working proxies (requires auth)
curl -H "Proxy-Authorization: Bearer token" http://localhost:8080/proxies
```

## Docker Deployment

```bash
# Using Docker Compose (recommended)
docker-compose up -d

# Set auth token in .env file
echo "APROXY_SERVER_AUTH_TOKEN=my-secret" > .env
docker-compose up -d

# Check logs
docker-compose logs -f
```

## How It Works

1. **Scraper** fetches proxy lists from multiple free sources (ProxyScrape, FreeProxyList)
2. **Health Checker** validates proxies using configurable test URLs
3. **Database** caches proxy health status to avoid redundant checks
4. **Manager** maintains pool of healthy proxies with automatic rotation
5. **Server** handles client requests using rotating proxy pool

## Privacy Features

- **Header stripping** - Removes `X-Forwarded-For`, `X-Real-IP`, etc.
- **User-Agent spoofing** - Randomizes browser identification
- **Connection sanitization** - Strips proxy-specific headers
- **HTTPS tunneling** - Supports CONNECT method for encrypted traffic

## Performance Optimizations

- **Intelligent caching** - Only re-checks proxies after configured interval (default 10min)
- **Batch processing** - Checks proxies in small batches to reduce system load
- **Concurrent workers** - Parallel health checking with configurable worker pools
- **Database persistence** - Proxy health survives application restarts
- **In-memory cache** - Fast access to working proxies

## Use Cases

**Penetration Testing:**
- Web application security testing through different IP ranges
- Bypassing IP-based rate limiting during security assessments
- Testing geolocation-based security controls

**Privacy Research:**
- Anonymous browsing for research purposes
- Testing website behavior across different geographic locations
- Avoiding tracking and fingerprinting

**Development & Testing:**
- Testing applications with different IP addresses
- Load testing from multiple source IPs
- API testing without rate limiting

## Configuration Options

### Server
- `server.listen_addr` - Bind address (default: `:8080`)
- `server.auth_token` - Optional Bearer token for authentication
- `server.max_connections` - Max concurrent connections (default: `1000`)

### Health Checking  
- `checker.check_interval` - Min time between proxy checks (default: `10m`)
- `checker.timeout` - Proxy test timeout (default: `15s`)
- `checker.batch_size` - Proxies per batch (default: `50`)
- `checker.batch_delay` - Delay between batches (default: `30s`)

### Database
- `database.path` - SQLite file location (default: `./data/aproxy.db`)
- `database.cleanup_interval` - How often to remove old proxies (default: `1h`)
- `database.max_age` - Max age before proxy cleanup (default: `24h`)

## Monitoring

```bash
# Check if server is healthy
curl http://localhost:8080/health

# Get detailed statistics
curl -H "Proxy-Authorization: Bearer token" http://localhost:8080/stats | jq

# Monitor proxy count
watch -n 5 'curl -s http://localhost:8080/health'
```

## Security Considerations

- **Free proxy risks** - Free proxies may log traffic or inject content
- **Authentication recommended** - Use `auth_token` to prevent unauthorized access
- **HTTPS for sensitive data** - Proxy doesn't encrypt traffic itself
- **Regular monitoring** - Check proxy health and statistics regularly
- **Rate limiting** - Consider implementing additional rate limiting for production

## Development

```bash
# Build
go build -o aproxy ./cmd/aproxy

# Run tests
go test ./...

# Format code
go fmt ./...

# Build with race detection
go build -race -o aproxy ./cmd/aproxy
```

## License

MIT License - see LICENSE file for details.

## Disclaimer

This tool is intended for legitimate security research, penetration testing, and privacy research. Users are responsible for compliance with applicable laws and regulations. Not recommended for production traffic or sensitive data.