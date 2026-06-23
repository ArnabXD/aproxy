package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"aproxy/internal/config"
	"aproxy/internal/database"
	"aproxy/internal/logger"
	"aproxy/pkg/manager"
	"aproxy/pkg/proxy"
)

var (
	configPath = flag.String("config", "", "Path to config file")
	genConfig  = flag.Bool("gen-config", false, "Generate default config file")
	version    = flag.Bool("version", false, "Show version")
)

var (
	Version = "1.0.0"
)

const (
	Banner = `
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

	log := logger.New("main")

	if *genConfig {
		if err := config.SaveConfigTemplate("config.yaml"); err != nil {
			log.Fatal("Failed to generate config: %v", err)
		}
		fmt.Println("Default config generated: config.yaml")
		return
	}

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatal("Failed to load config: %v", err)
	}

	log.InfoBg("Starting AProxy v%s", Version)
	config.PrintConfig(cfg)

	// Initialize database
	db, err := database.NewDB(cfg.Database.Path)
	if err != nil {
		log.Fatal("Failed to initialize database: %v", err)
	}
	defer db.Close()

	mgr := manager.NewDBManager(db, cfg)
	if err := mgr.Start(cfg.Proxy.UpdateInterval); err != nil {
		log.Fatal("Failed to start proxy manager: %v", err)
	}

	server := proxy.NewServer(mgr, cfg.Server)

	go func() {
		if err := server.Start(); err != nil {
			log.ErrorBg("Server error: %v", err)
		}
	}()

	log.InfoBg("Proxy server started on %s", cfg.Server.ListenAddr)
	log.InfoBg("Press Ctrl+C to stop")

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c
	log.InfoBg("Shutting down...")

	// Stop the manager first to cancel background operations
	mgr.Stop()

	// Shorter timeout for server shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Stop(ctx); err != nil {
		log.ErrorBg("Server shutdown error: %v", err)
	}

	log.InfoBg("Shutdown complete")
}
