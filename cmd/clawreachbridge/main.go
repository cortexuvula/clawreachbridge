package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/v22/daemon"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"github.com/cortexuvula/clawreachbridge/internal/config"
	"github.com/cortexuvula/clawreachbridge/internal/health"
	"github.com/cortexuvula/clawreachbridge/internal/logging"
	"github.com/cortexuvula/clawreachbridge/internal/metrics"
	"github.com/cortexuvula/clawreachbridge/internal/proxy"
	"github.com/cortexuvula/clawreachbridge/internal/security"
	"github.com/cortexuvula/clawreachbridge/internal/setup"

	"golang.org/x/time/rate"
)

// Build-time variables set via ldflags.
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "clawreachbridge",
		Short: "Secure WebSocket proxy for ClawReach ↔ OpenClaw Gateway over Tailscale",
	}

	var configPath string
	var verbose bool
	var foreground bool

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the WebSocket proxy bridge",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBridge(configPath, verbose)
		},
	}
	startCmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config file")
	startCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable debug logging")
	startCmd.Flags().BoolVar(&foreground, "foreground", false, "Run in foreground (implied)")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Show version and build info",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("ClawReach Bridge %s\n", Version)
			fmt.Printf("  Build time: %s\n", BuildTime)
			fmt.Printf("  Git commit: %s\n", GitCommit)
		},
	}

	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate config without starting",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("config validation failed: %w", err)
			}
			fmt.Printf("Configuration is valid.\n")
			fmt.Printf("  Listen: %s\n", cfg.Bridge.ListenAddress)
			fmt.Printf("  Gateway: %s\n", cfg.Bridge.GatewayURL)
			fmt.Printf("  Health: %s\n", cfg.Health.ListenAddress)
			fmt.Printf("  Tailscale only: %v\n", cfg.Security.TailscaleOnly)
			return nil
		},
	}
	validateCmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config file")

	healthCmd := &cobra.Command{
		Use:   "health",
		Short: "Check health (exit 0 if healthy, 1 if not)",
		RunE: func(cmd *cobra.Command, args []string) error {
			url, _ := cmd.Flags().GetString("url")
			return checkHealth(url)
		},
	}
	healthCmd.Flags().String("url", "http://127.0.0.1:8081/health", "Health endpoint URL")

	var setupConfigPath string
	setupCmd := &cobra.Command{
		Use:   "setup",
		Short: "Interactive setup wizard",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setup.RunWizard(os.Stdin, os.Stdout, setup.WizardOptions{
				ConfigPath: setupConfigPath,
			})
		},
	}
	setupCmd.Flags().StringVar(&setupConfigPath, "config-path", "", "Override config file path (default: /etc/clawreachbridge/config.yaml)")

	systemdCmd := &cobra.Command{
		Use:   "systemd",
		Short: "Generate systemd service file",
		RunE: func(cmd *cobra.Command, args []string) error {
			printFlag, _ := cmd.Flags().GetBool("print")
			if printFlag {
				printSystemdUnit()
			}
			return nil
		},
	}
	systemdCmd.Flags().Bool("print", false, "Print systemd unit to stdout")

	rootCmd.AddCommand(startCmd, versionCmd, validateCmd, healthCmd, setupCmd, systemdCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runBridge(configPath string, verbose bool) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if verbose {
		cfg.Logging.Level = "debug"
	}

	// Set up logging
	lj := logging.Setup(
		cfg.Logging.Level,
		cfg.Logging.Format,
		cfg.Logging.File,
		cfg.Logging.MaxSizeMB,
		cfg.Logging.MaxBackups,
		cfg.Logging.MaxAgeDays,
		cfg.Logging.Compress,
	)
	if lj != nil {
		defer lj.Close()
	}

	slog.Info("starting ClawReach Bridge",
		"version", Version,
		"listen", cfg.Bridge.ListenAddress,
		"gateway", cfg.Bridge.GatewayURL,
		"health", cfg.Health.ListenAddress,
	)

	// Create proxy and rate limiter
	p := proxy.New()

	var rl *security.RateLimiter
	if cfg.Security.RateLimit.Enabled {
		r := rate.Limit(float64(cfg.Security.RateLimit.ConnectionsPerMinute) / 60.0)
		rl = security.NewRateLimiter(r, cfg.Security.RateLimit.ConnectionsPerMinute)
		defer rl.Stop()
		slog.Info("rate limiting enabled",
			"connections_per_minute", cfg.Security.RateLimit.ConnectionsPerMinute,
		)
	}

	// Create proxy handler
	handler := proxy.NewHandler(cfg, p, rl)

	// Optional Prometheus metrics
	var m *metrics.Metrics
	if cfg.Monitoring.MetricsEnabled {
		m = metrics.New()
		handler.Metrics = m
		slog.Info("prometheus metrics enabled", "endpoint", cfg.Monitoring.MetricsEndpoint)
	}

	// Proxy server (listens on Tailscale IP)
	proxyServer := &http.Server{
		Addr:    cfg.Bridge.ListenAddress,
		Handler: handler,
	}

	// Health server (listens on 127.0.0.1:8081)
	var healthServer *http.Server
	if cfg.Health.Enabled {
		healthHandler := health.NewHandler(p, cfg.Bridge.GatewayURL, Version)
		if m != nil {
			healthHandler.SetMetrics(m)
		}
		healthMux := http.NewServeMux()
		healthMux.Handle(cfg.Health.Endpoint, healthHandler)

		// Metrics endpoint on health listener
		if cfg.Monitoring.MetricsEnabled {
			healthMux.Handle(cfg.Monitoring.MetricsEndpoint, promhttp.Handler())
		}

		healthServer = &http.Server{
			Addr:    cfg.Health.ListenAddress,
			Handler: healthMux,
		}
	}

	// Start health server
	if healthServer != nil {
		go func() {
			slog.Info("health endpoint listening", "address", cfg.Health.ListenAddress)
			if err := healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("health server error", "error", err)
			}
		}()
	}

	// Start proxy server
	go func() {
		slog.Info("proxy listening", "address", cfg.Bridge.ListenAddress)
		if err := proxyServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("proxy server error", "error", err)
		}
	}()

	// Notify systemd that we're ready
	daemon.SdNotify(false, daemon.SdNotifyReady)

	// Start watchdog heartbeat (send every 15s for 30s WatchdogSec)
	watchdogCtx, watchdogCancel := context.WithCancel(context.Background())
	defer watchdogCancel()
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sent, err := daemon.SdNotify(false, daemon.SdNotifyWatchdog)
				if err != nil {
					slog.Warn("failed to notify watchdog", "error", err)
				} else if sent {
					slog.Debug("watchdog keepalive sent")
				}
			case <-watchdogCtx.Done():
				return
			}
		}
	}()

	// Signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

	for sig := range sigChan {
		switch sig {
		case syscall.SIGHUP:
			slog.Info("received SIGHUP, reloading config")
			newCfg, err := config.Load(configPath)
			if err != nil {
				slog.Error("config reload failed", "error", err)
				continue
			}

			warnings := config.IsReloadSafe(cfg, newCfg)
			for _, w := range warnings {
				slog.Warn("config reload warning", "warning", w)
			}

			// Apply reloadable fields
			cfg.ReloadableFields(newCfg)
			handler.UpdateConfig(cfg)

			// Update rate limiter
			if cfg.Security.RateLimit.Enabled && rl != nil {
				r := rate.Limit(float64(cfg.Security.RateLimit.ConnectionsPerMinute) / 60.0)
				rl.UpdateRate(r, cfg.Security.RateLimit.ConnectionsPerMinute)
			}

			// Re-setup logging with new level
			logging.Setup(
				cfg.Logging.Level,
				cfg.Logging.Format,
				cfg.Logging.File,
				cfg.Logging.MaxSizeMB,
				cfg.Logging.MaxBackups,
				cfg.Logging.MaxAgeDays,
				cfg.Logging.Compress,
			)

			slog.Info("config reloaded successfully")

		case syscall.SIGTERM, syscall.SIGINT:
			slog.Info("received shutdown signal, draining connections",
				"signal", sig.String(),
				"drain_timeout", cfg.Bridge.DrainTimeout.String(),
			)

			// Stop watchdog
			watchdogCancel()
			daemon.SdNotify(false, daemon.SdNotifyStopping)

			// Stop accepting new connections
			ctx, cancel := context.WithTimeout(context.Background(), cfg.Bridge.DrainTimeout)
			defer cancel()

			var wg sync.WaitGroup
			if healthServer != nil {
				wg.Add(1)
				go func() {
					defer wg.Done()
					healthServer.Shutdown(ctx)
				}()
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				proxyServer.Shutdown(ctx)
			}()
			wg.Wait()

			slog.Info("shutdown complete")
			return nil
		}
	}

	return nil
}

func checkHealth(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Health check failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Println("healthy")
		return nil
	}
	fmt.Fprintf(os.Stderr, "unhealthy (status: %d)\n", resp.StatusCode)
	os.Exit(1)
	return nil
}


func printSystemdUnit() {
	fmt.Print(`[Unit]
Description=ClawReach Bridge - Secure WebSocket Proxy
Documentation=https://github.com/cortexuvula/clawreachbridge
After=network-online.target tailscaled.service
Wants=network-online.target
Requires=tailscaled.service

[Service]
Type=notify
User=clawreachbridge
Group=clawreachbridge
ExecStartPre=/usr/local/bin/clawreachbridge validate --config /etc/clawreachbridge/config.yaml
ExecStart=/usr/local/bin/clawreachbridge start --config /etc/clawreachbridge/config.yaml
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5s
WatchdogSec=30s

# Security hardening
ProtectSystem=strict
ProtectHome=true
NoNewPrivileges=true
PrivateTmp=true
ReadOnlyPaths=/etc/clawreachbridge
LogsDirectory=clawreachbridge
StateDirectory=clawreachbridge
LimitNOFILE=65535

# Memory safety net: ~15MB base + ~20KB/connection × 1000 max = ~35MB typical
# Set headroom for message buffering spikes (max_message_size × active conns)
MemoryMax=128M

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=clawreachbridge

[Install]
WantedBy=multi-user.target
`)
}
