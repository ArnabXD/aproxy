package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"aproxy/internal/config"
	"aproxy/internal/database"
	"aproxy/pkg/checker"
	"aproxy/pkg/manager"
	"aproxy/pkg/proxy"
	"aproxy/pkg/scraper"
)

var (
	configPath = flag.String("config", "", "Path to config file")
	genConfig  = flag.Bool("gen-config", false, "Generate default config file")
	version    = flag.Bool("version", false, "Show version")
)

const (
	Version = "1.0.0"
	Banner  = `
______ ______ ______ ______ ______ ______ ______ ______

 ▄▄▄       ██▓███   ██▀███   ▒█████  ▒██   ██▒▓██   ██▓
▒████▄    ▓██░  ██▒▓██ ▒ ██▒▒██▒  ██▒▒▒ █ █ ▒░ ▒██  ██▒
▒██  ▀█▄  ▓██░ ██▓▒▓██ ░▄█ ▒▒██░  ██▒░░  █   ░  ▒██ ██░
░██▄▄▄▄██ ▒██▄█▓▒ ▒▒██▀▀█▄  ▒██   ██░ ░ █ █ ▒   ░ ▐██▓░
 ▓█   ▓██▒▒██▒ ░  ░░██▓ ▒██▒░ ████▓▒░▒██▒ ▒██▒  ░ ██▒▓░
 ▒▒   ▓▒█░▒▓▒░ ░  ░░ ▒▓ ░▒▓░░ ▒░▒░▒░ ▒▒ ░ ░▓ ░   ██▒▒▒ 
  ▒   ▒▒ ░░▒ ░       ░▒ ░ ▒░  ░ ▒ ▒░ ░░   ░▒ ░ ▓██ ░▒░ 
  ░   ▒   ░░         ░░   ░ ░ ░ ░ ▒   ░    ░   ▒ ▒ ░░  
      ░  ░            ░         ░ ░   ░    ░   ░ ░     
                                               ░ ░     
______ ______ ______ ______ ______ ______ ______ ______

AProxy - Anonymous Proxy Server v%s
https://github.com/ArnabXD/aproxy

______ ______ ______ ______ ______ ______ ______ ______

`
)

func main() {
	flag.Parse()

	if *version {
		fmt.Printf("AProxy v%s\n", Version)
		return
	}

	fmt.Printf(Banner, Version)

	if *genConfig {
		if err := config.SaveConfigTemplate("config.yaml"); err != nil {
			log.Fatalf("Failed to generate config: %v", err)
		}
		fmt.Println("Default config generated: config.yaml")
		return
	}

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Starting AProxy v%s", Version)
	config.PrintConfig(cfg)

	// Initialize database
	db, err := database.NewDB(cfg.Database.Path)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Create configuration objects for checker and scraper
	scraperConfig := scraper.ScraperConfig{
		Timeout:   cfg.Scraper.Timeout,
		UserAgent: cfg.Scraper.UserAgent,
		Sources:   cfg.Scraper.Sources,
	}
	
	checkerConfig := checker.CheckerConfig{
		TestURL:    cfg.Checker.TestURL,
		Timeout:    cfg.Checker.Timeout,
		MaxWorkers: cfg.Checker.MaxWorkers,
		UserAgent:  cfg.Checker.UserAgent,
	}

	// Use database manager with configuration
	mgr := manager.NewDBManagerWithConfig(db, scraperConfig, checkerConfig, cfg.Checker.CheckInterval, cfg.Checker.BackgroundEnabled, cfg.Checker.BatchSize, cfg.Checker.BatchDelay)
	if err := mgr.Start(cfg.Proxy.UpdateInterval); err != nil {
		log.Fatalf("Failed to start proxy manager: %v", err)
	}

	proxyConfig := &proxy.Config{
		ListenAddr:     cfg.Server.ListenAddr,
		ReadTimeout:    cfg.Server.ReadTimeout,
		WriteTimeout:   cfg.Server.WriteTimeout,
		IdleTimeout:    cfg.Server.IdleTimeout,
		MaxConnections: cfg.Server.MaxConnections,
		EnableHTTPS:    cfg.Server.EnableHTTPS,
		MaxRetries:     cfg.Server.MaxRetries,
		StripHeaders:   cfg.Server.StripHeaders,
		AddHeaders:     cfg.Server.AddHeaders,
	}

	server := proxy.NewServer(mgr, proxyConfig)

	go func() {
		if err := server.Start(); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	log.Printf("Proxy server started on %s", cfg.Server.ListenAddr)
	log.Println("Press Ctrl+C to stop")

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c
	log.Println("Shutting down...")

	// Stop the manager first to cancel background operations
	mgr.Stop()

	// Shorter timeout for server shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Stop(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Shutdown complete")
}
