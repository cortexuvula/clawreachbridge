package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/v22/daemon"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"github.com/cortexuvula/clawreachbridge/internal/canvas"
	"github.com/cortexuvula/clawreachbridge/internal/chatsync"
	"github.com/cortexuvula/clawreachbridge/internal/config"
	"github.com/cortexuvula/clawreachbridge/internal/health"
	"github.com/cortexuvula/clawreachbridge/internal/logging"
	"github.com/cortexuvula/clawreachbridge/internal/logring"
	"github.com/cortexuvula/clawreachbridge/internal/metrics"
	"github.com/cortexuvula/clawreachbridge/internal/proxy"
	"github.com/cortexuvula/clawreachbridge/internal/security"
	"github.com/cortexuvula/clawreachbridge/internal/setup"
	"github.com/cortexuvula/clawreachbridge/internal/webui"

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

	// Set up logging with ring buffer for web UI log viewer
	ring := logring.NewRingBuffer(1000)
	baseHandler, lj := logging.SetupHandler(
		cfg.Logging.Level,
		cfg.Logging.Format,
		cfg.Logging.File,
		cfg.Logging.MaxSizeMB,
		cfg.Logging.MaxBackups,
		cfg.Logging.MaxAgeDays,
		cfg.Logging.Compress,
	)
	slog.SetDefault(slog.New(logring.NewTeeHandler(baseHandler, ring)))
	if lj != nil {
		defer lj.Close()
	}

	startTime := time.Now()

	slog.Info("starting ClawReach Bridge",
		"version", Version,
		"listen", cfg.Bridge.ListenAddress,
		"gateway", cfg.Bridge.GatewayURL,
		"health", cfg.Health.ListenAddress,
	)

	// Create shutdown context (cancelled on SIGTERM/SIGINT to tear down active connections)
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	defer shutdownCancel()

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
	handler := proxy.NewHandler(cfg, p, rl, shutdownCtx)

	// Optional Prometheus metrics
	var m *metrics.Metrics
	if cfg.Monitoring.MetricsEnabled {
		m = metrics.New()
		handler.Metrics = m
		slog.Info("prometheus metrics enabled", "endpoint", cfg.Monitoring.MetricsEndpoint)
	}

	// Optional reaction inspector (requires metrics for counting)
	if cfg.Bridge.Reactions.Enabled && m != nil {
		handler.ReactionInspector = proxy.NewReactionInspector(m.ReactionsTotal)
		slog.Info("reaction inspector enabled", "mode", cfg.Bridge.Reactions.Mode)
	}
	if cfg.Bridge.Reactions.Enabled && !cfg.Monitoring.MetricsEnabled {
		slog.Warn("reactions enabled but metrics disabled; reaction counting requires metrics")
	}

	// File receive inspector — saves uploaded files to agent workspace
	if cfg.Bridge.Media.Enabled && cfg.Bridge.Media.Directory != "" {
		inboxDir := filepath.Join(cfg.Bridge.Media.Directory, "inbox")
		if err := os.MkdirAll(inboxDir, 0755); err != nil {
			slog.Error("failed to create inbox directory", "path", inboxDir, "error", err)
		} else {
			handler.FileReceiveInspector = &proxy.FileReceiveInspector{
				InboxDir: inboxDir,
				Logger:   slog.Default().With("component", "file-receive"),
			}
			slog.Info("file receive inspector enabled", "inbox", inboxDir)
		}
	}

	// Optional canvas state tracking
	if cfg.Bridge.Canvas.StateTracking {
		tracker := canvas.NewTracker(cfg.Bridge.Canvas)
		if m != nil {
			tracker.SetMetrics(m.CanvasEventsTotal, m.CanvasReplaysTotal)
		}
		handler.CanvasTracker = tracker
		slog.Info("canvas state tracking enabled",
			"jsonl_buffer_size", cfg.Bridge.Canvas.JSONLBufferSize,
			"max_age", cfg.Bridge.Canvas.MaxAge,
		)
	}

	// Optional cross-device message sync
	if cfg.Bridge.Sync.Enabled {
		syncStore := chatsync.NewMessageStore(cfg.Bridge.Sync.MaxHistory)
		syncRegistry := chatsync.NewClientRegistry()
		handler.SyncStore = syncStore
		handler.SyncRegistry = syncRegistry
		slog.Info("cross-device message sync enabled", "max_history", cfg.Bridge.Sync.MaxHistory)
	}

	// Reload config closure — shared by SIGHUP handler and web UI
	reloadConfig := func() error {
		newCfg, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("config reload failed: %w", err)
		}

		warnings := config.IsReloadSafe(cfg, newCfg)
		for _, w := range warnings {
			slog.Warn("config reload warning", "warning", w)
		}

		cfg = cfg.ApplyReloadableFields(newCfg)
		handler.UpdateConfig(cfg)

		// Update rate limiter
		if cfg.Security.RateLimit.Enabled && rl != nil {
			r := rate.Limit(float64(cfg.Security.RateLimit.ConnectionsPerMinute) / 60.0)
			rl.UpdateRate(r, cfg.Security.RateLimit.ConnectionsPerMinute)
		}

		// Re-setup logging with new level, re-wrap with TeeHandler
		newHandler, _ := logging.SetupHandler(
			cfg.Logging.Level,
			cfg.Logging.Format,
			cfg.Logging.File,
			cfg.Logging.MaxSizeMB,
			cfg.Logging.MaxBackups,
			cfg.Logging.MaxAgeDays,
			cfg.Logging.Compress,
		)
		slog.SetDefault(slog.New(logring.NewTeeHandler(newHandler, ring)))

		slog.Info("config reloaded successfully")
		return nil
	}

	// Bind proxy listener synchronously (detect port conflicts before sd_notify)
	proxyListener, err := net.Listen("tcp", cfg.Bridge.ListenAddress)
	if err != nil {
		return fmt.Errorf("failed to bind proxy listener on %s: %w", cfg.Bridge.ListenAddress, err)
	}
	proxyServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Health server (listens on 127.0.0.1:8081)
	var healthServer *http.Server
	var healthListener net.Listener
	if cfg.Health.Enabled {
		healthHandler := health.NewHandler(p, cfg.Bridge.GatewayURL, Version, cfg.Health.Detailed)
		if m != nil {
			healthHandler.SetMetrics(m)
		}
		healthMux := http.NewServeMux()
		healthMux.Handle(cfg.Health.Endpoint, healthHandler)

		// Metrics endpoint on health listener
		if cfg.Monitoring.MetricsEnabled {
			healthMux.Handle(cfg.Monitoring.MetricsEndpoint, promhttp.Handler())
		}

		// Web admin UI on health listener
		adminUI := webui.New(webui.Dependencies{
			Proxy:       p,
			Handler:     handler,
			RateLimiter: rl,
			RingBuffer:  ring,
			Version:     Version,
			BuildTime:   BuildTime,
			GitCommit:   GitCommit,
			GatewayURL:  cfg.Bridge.GatewayURL,
			StartTime:   startTime,
			GetConfig:   func() *config.Config { return handler.GetConfig() },
			ReloadFunc:  reloadConfig,
		})
		healthMux.Handle("/ui/", adminUI.StaticHandler())
		healthMux.Handle("/api/v1/", adminUI.APIHandler())

		healthListener, err = net.Listen("tcp", cfg.Health.ListenAddress)
		if err != nil {
			proxyListener.Close()
			return fmt.Errorf("failed to bind health listener on %s: %w", cfg.Health.ListenAddress, err)
		}

		healthServer = &http.Server{
			Handler:           healthMux,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
		}
	}

	// Start health server (non-blocking)
	if healthServer != nil {
		go func() {
			slog.Info("health endpoint listening", "address", cfg.Health.ListenAddress)
			if err := healthServer.Serve(healthListener); err != nil && err != http.ErrServerClosed {
				slog.Error("health server error", "error", err)
			}
		}()
	}

	// Start proxy server (non-blocking)
	go func() {
		slog.Info("proxy listening", "address", cfg.Bridge.ListenAddress)
		if err := proxyServer.Serve(proxyListener); err != nil && err != http.ErrServerClosed {
			slog.Error("proxy server error", "error", err)
		}
	}()

	// Notify systemd that we're ready (both listeners are bound)
	sent, notifyErr := daemon.SdNotify(false, daemon.SdNotifyReady)
	if notifyErr != nil {
		slog.Error("sd_notify READY failed", "error", notifyErr)
	} else if !sent {
		slog.Warn("sd_notify READY not sent (NOTIFY_SOCKET not set — not running under systemd?)")
	} else {
		slog.Info("sd_notify READY sent")
	}

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
				} else {
					slog.Debug("watchdog notify skipped (NOTIFY_SOCKET not set)")
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
			if err := reloadConfig(); err != nil {
				slog.Error("config reload failed", "error", err)
			}

		case syscall.SIGTERM, syscall.SIGINT:
			slog.Info("received shutdown signal, draining connections",
				"signal", sig.String(),
				"drain_timeout", cfg.Bridge.DrainTimeout.String(),
			)

			// Stop watchdog and notify systemd
			watchdogCancel()
			daemon.SdNotify(false, daemon.SdNotifyStopping)

			// Phase 1: Stop accepting new connections + drain active ones
			proxyServer.Close() // immediately close listener

			handler.StartDrain() // send close frames to all active connections

			// Wait for active connections to finish (up to drain timeout)
			drainDeadline := time.After(cfg.Bridge.DrainTimeout)
			drainTick := time.NewTicker(100 * time.Millisecond)
		drainLoop:
			for {
				select {
				case <-drainDeadline:
					remaining := p.ConnectionCount()
					if remaining > 0 {
						slog.Warn("drain timeout reached, force-closing remaining connections", "remaining", remaining)
					}
					break drainLoop
				case <-drainTick.C:
					if p.ConnectionCount() == 0 {
						slog.Info("all connections drained")
						break drainLoop
					}
				}
			}
			drainTick.Stop()

			// Phase 2: Force-close anything remaining
			shutdownCancel()

			// Shutdown health server
			if healthServer != nil {
				shutdownCtx, shutdownCtxCancel := context.WithTimeout(context.Background(), 5*time.Second)
				healthServer.Shutdown(shutdownCtx)
				shutdownCtxCancel()
			}

			slog.Info("shutdown complete")
			return nil
		}
	}

	return nil
}

func checkHealth(healthURL string) error {
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(healthURL)
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
Restart=always
RestartPreventExitStatus=0
RestartSec=5s
WatchdogSec=30s
TimeoutStartSec=30s

# Security hardening
ProtectSystem=strict
ProtectHome=true
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
ProtectClock=true
RestrictNamespaces=true
RestrictRealtime=true
RestrictSUIDSGID=true
LockPersonality=true
SystemCallArchitectures=native
ReadOnlyPaths=/etc/clawreachbridge
LogsDirectory=clawreachbridge
StateDirectory=clawreachbridge
LimitNOFILE=65535

# Memory safety net: ~15MB base + ~20KB/connection × 1000 max = ~35MB typical
# Set headroom for message buffering spikes (max_message_size=256KB × active conns)
MemoryMax=128M

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=clawreachbridge

[Install]
WantedBy=multi-user.target
`)
}
