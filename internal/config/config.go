package config

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server" validate:"required"`
	Proxy    ProxyConfig    `mapstructure:"proxy" validate:"required"`
	Scraper  ScraperConfig  `mapstructure:"scraper" validate:"required"`
	Checker  CheckerConfig  `mapstructure:"checker" validate:"required"`
	Database DatabaseConfig `mapstructure:"database" validate:"required"`
}

type ServerConfig struct {
	ListenAddr     string            `mapstructure:"listen_addr" validate:"required,hostname_port"`
	ReadTimeout    time.Duration     `mapstructure:"read_timeout" validate:"required,min=1s,max=5m"`
	WriteTimeout   time.Duration     `mapstructure:"write_timeout" validate:"required,min=1s,max=5m"`
	IdleTimeout    time.Duration     `mapstructure:"idle_timeout" validate:"required,min=1s,max=10m"`
	MaxConnections int               `mapstructure:"max_connections" validate:"required,min=1,max=10000"`
	EnableHTTPS    bool              `mapstructure:"enable_https"`
	MaxRetries     int               `mapstructure:"max_retries" validate:"required,min=1,max=10"`
	StripHeaders   []string          `mapstructure:"strip_headers"`
	AddHeaders     map[string]string `mapstructure:"add_headers"`
	AuthToken      string            `mapstructure:"auth_token"`
}

type ProxyConfig struct {
	UpdateInterval time.Duration `mapstructure:"update_interval" validate:"required,min=1m,max=24h"`
	MaxFailures    int           `mapstructure:"max_failures" validate:"required,min=1,max=100"`
	RecheckTime    time.Duration `mapstructure:"recheck_time" validate:"required,min=1m,max=1h"`
}

type ScraperConfig struct {
	Timeout   time.Duration `mapstructure:"timeout" validate:"required,min=5s,max=2m"`
	UserAgent string        `mapstructure:"user_agent" validate:"required,min=10"`
	Sources   []string      `mapstructure:"sources" validate:"required,min=1,dive,oneof=proxyscrape freeproxylist geonode proxylistorg"`
}

type CheckerConfig struct {
	TestURL           string        `mapstructure:"test_url" validate:"required,url"`
	Timeout           time.Duration `mapstructure:"timeout" validate:"required,min=5s,max=1m"`
	MaxWorkers        int           `mapstructure:"max_workers" validate:"required,min=1,max=200"`
	UserAgent         string        `mapstructure:"user_agent" validate:"required,min=10"`
	CheckInterval     time.Duration `mapstructure:"check_interval" validate:"required,min=1m,max=1h"`
	BatchSize         int           `mapstructure:"batch_size" validate:"required,min=10,max=500"`
	BatchDelay        time.Duration `mapstructure:"batch_delay" validate:"required,min=5s,max=5m"`
	BackgroundEnabled bool          `mapstructure:"background_enabled"`
}

type DatabaseConfig struct {
	Path            string        `mapstructure:"path" validate:"required,min=1"`
	MaxAge          time.Duration `mapstructure:"max_age" validate:"required,min=1h,max=168h"`
	CleanupInterval time.Duration `mapstructure:"cleanup_interval" validate:"required,min=30m,max=24h"`
}

// setDefaults configures default values for viper
func setDefaults() {
	// Server defaults
	viper.SetDefault("server.listen_addr", ":8080")
	viper.SetDefault("server.read_timeout", "30s")
	viper.SetDefault("server.write_timeout", "30s")
	viper.SetDefault("server.idle_timeout", "60s")
	viper.SetDefault("server.max_connections", 1000)
	viper.SetDefault("server.enable_https", true)
	viper.SetDefault("server.max_retries", 3)
	viper.SetDefault("server.strip_headers", []string{
		"X-Forwarded-For", "X-Real-IP", "X-Original-IP", "CF-Connecting-IP", "True-Client-IP",
	})
	viper.SetDefault("server.add_headers", map[string]string{
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
	})
	viper.SetDefault("server.auth_token", "")

	// Proxy defaults
	viper.SetDefault("proxy.update_interval", "15m")
	viper.SetDefault("proxy.max_failures", 3)
	viper.SetDefault("proxy.recheck_time", "5m")

	// Scraper defaults
	viper.SetDefault("scraper.timeout", "30s")
	viper.SetDefault("scraper.user_agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	viper.SetDefault("scraper.sources", []string{"proxyscrape", "freeproxylist", "geonode"})

	// Checker defaults
	viper.SetDefault("checker.test_url", "http://icanhazip.com")
	viper.SetDefault("checker.timeout", "15s")
	viper.SetDefault("checker.max_workers", 50)
	viper.SetDefault("checker.user_agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	viper.SetDefault("checker.check_interval", "10m")
	viper.SetDefault("checker.batch_size", 50)
	viper.SetDefault("checker.batch_delay", "30s")
	viper.SetDefault("checker.background_enabled", true)

	// Database defaults
	viper.SetDefault("database.path", "./data/aproxy.db")
	viper.SetDefault("database.max_age", "24h")
	viper.SetDefault("database.cleanup_interval", "1h")

}

// LoadConfig loads configuration from multiple sources with validation
func LoadConfig(configPath string) (*Config, error) {
	// Configure viper
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")
	viper.AddConfigPath("/etc/aproxy")

	// Set environment variable prefix and enable reading from env
	viper.SetEnvPrefix("APROXY")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// Set defaults
	setDefaults()

	// Load .env file if it exists (for backward compatibility)
	if _, err := os.Stat(".env"); err == nil {
		viper.SetConfigFile(".env")
		viper.SetConfigType("env")
		if err := viper.MergeInConfig(); err != nil {
			log.Printf("Warning: Failed to load .env file: %v", err)
		}
	}

	// Try to read config file if provided or found
	if configPath != "" {
		viper.SetConfigFile(configPath)
	}

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		// Config file not found, use defaults and env vars
		log.Println("No config file found, using defaults and environment variables")
	}

	// Unmarshal configuration
	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate configuration
	validate := validator.New()

	// Register custom validators
	if err := registerCustomValidators(validate); err != nil {
		return nil, fmt.Errorf("failed to register validators: %w", err)
	}

	if err := validate.Struct(&config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

// registerCustomValidators adds custom validation rules
func registerCustomValidators(validate *validator.Validate) error {
	// Custom validator for hostname:port format
	return validate.RegisterValidation("hostname_port", func(fl validator.FieldLevel) bool {
		addr := fl.Field().String()
		if addr == "" {
			return false
		}
		// Simple check for :port format
		return strings.Contains(addr, ":")
	})
}

// SaveConfigTemplate generates a sample configuration file
func SaveConfigTemplate(path string) error {
	setDefaults()
	viper.SetConfigType("yaml")

	if err := viper.SafeWriteConfigAs(path); err != nil {
		return fmt.Errorf("failed to write config template: %w", err)
	}

	return nil
}

// PrintConfig displays the current configuration (for debugging)
func PrintConfig(config *Config) {
	log.Printf("Configuration loaded:")
	log.Printf("  Server: %s (HTTPS: %v)", config.Server.ListenAddr, config.Server.EnableHTTPS)
	if config.Server.AuthToken != "" {
		log.Printf("  Auth Token: [SET] (length: %d)", len(config.Server.AuthToken))
	} else {
		log.Printf("  Auth Token: [NOT SET]")
	}
	log.Printf("  Database: %s (Max Age: %v)", config.Database.Path, config.Database.MaxAge)
	log.Printf("  Proxy Update: %v (Max Failures: %d)", config.Proxy.UpdateInterval, config.Proxy.MaxFailures)
	log.Printf("  Checker: %d workers, %v timeout, batch size: %d, batch delay: %v, background: %v",
		config.Checker.MaxWorkers, config.Checker.Timeout, config.Checker.BatchSize, config.Checker.BatchDelay, config.Checker.BackgroundEnabled)
	log.Printf("  Scraper Sources: %v", config.Scraper.Sources)
}
