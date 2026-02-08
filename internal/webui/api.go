package webui

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"time"
)

// statusResponse is the JSON body for GET /api/v1/status.
type statusResponse struct {
	Uptime            string  `json:"uptime"`
	UptimeSeconds     float64 `json:"uptime_seconds"`
	ActiveConnections int     `json:"active_connections"`
	TotalConnections  int64   `json:"total_connections"`
	TotalMessages     int64   `json:"total_messages"`
	GatewayReachable  bool    `json:"gateway_reachable"`
	MemoryMB          float64 `json:"memory_mb"`
	Goroutines        int     `json:"goroutines"`
	Version           string  `json:"version"`
	BuildTime         string  `json:"build_time"`
	GitCommit         string  `json:"git_commit"`
}

func (ui *WebUI) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	uptime := time.Since(ui.deps.StartTime)

	resp := statusResponse{
		Uptime:            uptime.Round(time.Second).String(),
		UptimeSeconds:     uptime.Seconds(),
		ActiveConnections: ui.deps.Proxy.ConnectionCount(),
		TotalConnections:  ui.deps.Proxy.TotalConnections(),
		TotalMessages:     ui.deps.Proxy.TotalMessages(),
		GatewayReachable:  checkGatewayReachable(ui.deps.GatewayURL),
		MemoryMB:          float64(memStats.Alloc) / 1024 / 1024,
		Goroutines:        runtime.NumGoroutine(),
		Version:           ui.deps.Version,
		BuildTime:         ui.deps.BuildTime,
		GitCommit:         ui.deps.GitCommit,
	}

	writeJSON(w, http.StatusOK, resp)
}

// connectionEntry represents a per-IP connection entry.
type connectionEntry struct {
	IP    string `json:"ip"`
	Count int    `json:"count"`
}

func (ui *WebUI) handleConnections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	ipMap := ui.deps.Proxy.ActiveIPConnections()
	entries := make([]connectionEntry, 0, len(ipMap))
	for ip, count := range ipMap {
		entries = append(entries, connectionEntry{IP: ip, Count: count})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Count > entries[j].Count
	})

	writeJSON(w, http.StatusOK, entries)
}

// configResponse is the JSON body for GET /api/v1/config.
type configResponse struct {
	Reloadable configReloadable `json:"reloadable"`
	ReadOnly   configReadOnly   `json:"read_only"`
}

type configReloadable struct {
	LogLevel            string `json:"log_level"`
	MaxConnections      int    `json:"max_connections"`
	MaxConnectionsPerIP int    `json:"max_connections_per_ip"`
	MaxMessageSize      int64  `json:"max_message_size"`
	RateLimitEnabled    bool   `json:"rate_limit_enabled"`
	ConnectionsPerMin   int    `json:"connections_per_minute"`
	MessagesPerSecond   int    `json:"messages_per_second"`
	AuthTokenSet        bool   `json:"auth_token_set"`
}

type configReadOnly struct {
	ListenAddress string `json:"listen_address"`
	GatewayURL    string `json:"gateway_url"`
	Origin        string `json:"origin"`
	HealthAddress string `json:"health_address"`
	TailscaleOnly bool   `json:"tailscale_only"`
	TLSEnabled    bool   `json:"tls_enabled"`
}

func (ui *WebUI) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ui.handleConfigGet(w, r)
	case http.MethodPut:
		ui.handleConfigPut(w, r)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (ui *WebUI) handleConfigGet(w http.ResponseWriter, _ *http.Request) {
	cfg := ui.deps.GetConfig()

	resp := configResponse{
		Reloadable: configReloadable{
			LogLevel:            cfg.Logging.Level,
			MaxConnections:      cfg.Security.MaxConnections,
			MaxConnectionsPerIP: cfg.Security.MaxConnectionsPerIP,
			MaxMessageSize:      cfg.Bridge.MaxMessageSize,
			RateLimitEnabled:    cfg.Security.RateLimit.Enabled,
			ConnectionsPerMin:   cfg.Security.RateLimit.ConnectionsPerMinute,
			MessagesPerSecond:   cfg.Security.RateLimit.MessagesPerSecond,
			AuthTokenSet:        cfg.Security.AuthToken != "",
		},
		ReadOnly: configReadOnly{
			ListenAddress: cfg.Bridge.ListenAddress,
			GatewayURL:    cfg.Bridge.GatewayURL,
			Origin:        cfg.Bridge.Origin,
			HealthAddress: cfg.Health.ListenAddress,
			TailscaleOnly: cfg.Security.TailscaleOnly,
			TLSEnabled:    cfg.Bridge.TLS.Enabled,
		},
	}

	writeJSON(w, http.StatusOK, resp)
}

// configUpdateRequest is the JSON body for PUT /api/v1/config.
type configUpdateRequest struct {
	LogLevel            *string `json:"log_level,omitempty"`
	MaxConnections      *int    `json:"max_connections,omitempty"`
	MaxConnectionsPerIP *int    `json:"max_connections_per_ip,omitempty"`
	MaxMessageSize      *int64  `json:"max_message_size,omitempty"`
	RateLimitEnabled    *bool   `json:"rate_limit_enabled,omitempty"`
	ConnectionsPerMin   *int    `json:"connections_per_minute,omitempty"`
	MessagesPerSecond   *int    `json:"messages_per_second,omitempty"`
}

func (ui *WebUI) handleConfigPut(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	var req configUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	cfg := ui.deps.GetConfig()

	// Apply updates to a copy
	updated := *cfg

	if req.LogLevel != nil {
		switch *req.LogLevel {
		case "debug", "info", "warn", "error":
			updated.Logging.Level = *req.LogLevel
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "log_level must be debug, info, warn, or error"})
			return
		}
	}
	if req.MaxConnections != nil {
		if *req.MaxConnections <= 0 || *req.MaxConnections > 65535 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "max_connections must be 1-65535"})
			return
		}
		updated.Security.MaxConnections = *req.MaxConnections
	}
	if req.MaxConnectionsPerIP != nil {
		if *req.MaxConnectionsPerIP <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "max_connections_per_ip must be positive"})
			return
		}
		updated.Security.MaxConnectionsPerIP = *req.MaxConnectionsPerIP
	}
	if req.MaxMessageSize != nil {
		if *req.MaxMessageSize <= 0 || *req.MaxMessageSize > 67108864 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "max_message_size must be 1 to 67108864"})
			return
		}
		updated.Bridge.MaxMessageSize = *req.MaxMessageSize
	}
	if req.RateLimitEnabled != nil {
		updated.Security.RateLimit.Enabled = *req.RateLimitEnabled
	}
	if req.ConnectionsPerMin != nil {
		if *req.ConnectionsPerMin <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "connections_per_minute must be positive"})
			return
		}
		updated.Security.RateLimit.ConnectionsPerMinute = *req.ConnectionsPerMin
	}
	if req.MessagesPerSecond != nil {
		if *req.MessagesPerSecond <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "messages_per_second must be positive"})
			return
		}
		updated.Security.RateLimit.MessagesPerSecond = *req.MessagesPerSecond
	}

	// Validate cross-field constraint
	if updated.Security.MaxConnectionsPerIP > updated.Security.MaxConnections {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "max_connections_per_ip must not exceed max_connections"})
		return
	}

	ui.deps.Handler.UpdateConfig(&updated)
	slog.Info("config updated via web UI",
		"log_level", updated.Logging.Level,
		"max_connections", updated.Security.MaxConnections,
	)

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// logEntry mirrors logring.LogEntry for JSON serialization.
type logEntryResponse struct {
	Time    string         `json:"time"`
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

func (ui *WebUI) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	minLevel := slog.LevelDebug
	if v := r.URL.Query().Get("level"); v != "" {
		switch v {
		case "debug":
			minLevel = slog.LevelDebug
		case "info":
			minLevel = slog.LevelInfo
		case "warn":
			minLevel = slog.LevelWarn
		case "error":
			minLevel = slog.LevelError
		}
	}

	var since time.Time
	if v := r.URL.Query().Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			since = t
		}
	}

	entries := ui.deps.RingBuffer.Entries(limit, minLevel, since)
	resp := make([]logEntryResponse, len(entries))
	for i, e := range entries {
		resp[i] = logEntryResponse{
			Time:    e.Time.Format(time.RFC3339Nano),
			Level:   e.Level.String(),
			Message: e.Message,
			Attrs:   e.Attrs,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (ui *WebUI) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireJSON(w, r) {
		return
	}

	if ui.deps.ReloadFunc == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "reload not available"})
		return
	}

	if err := ui.deps.ReloadFunc(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
}

func (ui *WebUI) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireJSON(w, r) {
		return
	}

	slog.Warn("restart requested via web UI")
	writeJSON(w, http.StatusOK, map[string]string{"status": "restarting"})

	// Flush response before exiting
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Exit with code 1 so systemd Restart=always restarts us
	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(1)
	}()
}

// checkGatewayReachable does a quick HTTP check against the gateway.
var gatewayClient = &http.Client{
	Timeout: 3 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func checkGatewayReachable(gatewayURL string) bool {
	resp, err := gatewayClient.Get(gatewayURL)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// requireJSON checks that the Content-Type header is application/json.
// Returns false (and writes an error response) if the check fails.
func requireJSON(w http.ResponseWriter, r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	if ct != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
		return false
	}
	return true
}
