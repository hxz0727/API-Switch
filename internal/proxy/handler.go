package proxy

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hxz0727/API-Switch/internal/config"
	"github.com/hxz0727/API-Switch/internal/logutil"
	"github.com/hxz0727/API-Switch/internal/metrics"
	"github.com/hxz0727/API-Switch/internal/monitor"
	"github.com/hxz0727/API-Switch/internal/provider"
	"github.com/hxz0727/API-Switch/internal/resilience"
	"github.com/hxz0727/API-Switch/internal/streaming"
	"github.com/hxz0727/API-Switch/internal/usage"
	"github.com/hxz0727/API-Switch/pkg/anthropic"
)

// Server is the HTTP proxy server for Claude Code.
// It accepts Anthropic Messages API requests and routes them to the correct provider.
type Server struct {
	cfg           atomic.Value // *config.Config — safe for concurrent read
	cfgPath       string
	router        *Router
	tracker       *monitor.Tracker
	usageTracker  *usage.Tracker
	orchestrator  *resilience.Orchestrator
	metricsTracker *metrics.MetricsTracker
	mu            sync.Mutex
	httpServer    *http.Server
	done          chan struct{}
}

// getConfig returns the current config safely for concurrent reads.
func (s *Server) getConfig() *config.Config {
	return s.cfg.Load().(*config.Config)
}

// setConfig atomically updates the server config.
func (s *Server) setConfig(cfg *config.Config) {
	s.cfg.Store(cfg)
}

// DefaultUsagePath returns the default usage data file path.
func DefaultUsagePath() string {
	return filepath.Join(filepath.Dir(config.DefaultConfigPath()), ".api-switch", "usage.json")
}

// initUsageTracker loads or creates a usage tracker.
func initUsageTracker() *usage.Tracker {
	ut, err := usage.NewTracker(DefaultUsagePath())
	if err != nil {
		logutil.Warn("Failed to init usage tracker: %v", err)
	}
	return ut
}

// NewServer creates a new proxy server.
func NewServer(cfg *config.Config) *Server {
	s := &Server{
		router:         NewRouter(cfg),
		tracker:        monitor.NewTracker(1000),
		usageTracker:   initUsageTracker(),
		orchestrator:   resilience.NewOrchestrator(),
		metricsTracker: metrics.NewMetricsTracker(),
		done:           make(chan struct{}),
	}
	s.setConfig(cfg)
	return s
}

// authMiddleware checks for valid Bearer token if auth is configured.
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := s.getConfig()
		if cfg.Server.AuthToken == "" {
			next(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeAnthropicError(w, http.StatusUnauthorized, "authentication_error",
				"Missing Authorization header. Use: Authorization: Bearer <token>")
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			writeAnthropicError(w, http.StatusUnauthorized, "authentication_error",
				"Invalid Authorization header format. Use: Bearer <token>")
			return
		}

		token := parts[1]
		if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.Server.AuthToken)) != 1 {
			writeAnthropicError(w, http.StatusUnauthorized, "authentication_error",
				"Invalid authentication token")
			return
		}

		next(w, r)
	}
}

// StartWithConfigFile starts the HTTP server with config file path for hot-reload.
func (s *Server) StartWithConfigFile(addr string, cfgPath string) error {
	s.cfgPath = cfgPath

	mux := http.NewServeMux()
	handler := s.rateLimitMiddleware(s.authMiddleware(s.handleMessages))
	mux.HandleFunc("/v1/messages", handler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		cfg := s.getConfig()
		numModels := len(cfg.Models)
		numProviders := len(cfg.Providers)
		reqStats := s.tracker.Stats()["total_requests"]
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":           "ok",
			"models":           numModels,
			"providers":        numProviders,
			"requests":         reqStats,
			"circuit_breakers": s.orchestrator.GetBreakerStats(),
			"health":           s.orchestrator.GetHealthStatus(),
		})
	})
	// Prometheus metrics endpoint
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		s.metricsTracker.UpdateUptime()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprint(w, s.metricsTracker.GetCollector().Export())
	})
	mux.HandleFunc("/admin/reload", requireLocalhost(s.handleAdminReload))
	mux.HandleFunc("/admin/stats", requireLocalhost(s.handleAdminStats))
	mux.HandleFunc("/admin/events", requireLocalhost(s.handleAdminEvents))
	mux.HandleFunc("/admin/", requireLocalhost(s.handleAdminDashboard))

	if cfgPath != "" {
		go s.watchConfigFile(cfgPath)
	}

	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
	}

	cfg := s.getConfig()
	if cfg.Server.TLSCert != "" && cfg.Server.TLSKey != "" {
		logutil.Info("TLS enabled: cert=%s, key=%s", cfg.Server.TLSCert, cfg.Server.TLSKey)
		return s.httpServer.ListenAndServeTLS(cfg.Server.TLSCert, cfg.Server.TLSKey)
	}

	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server and cleans up resources.
func (s *Server) Shutdown(ctx context.Context) error {
	close(s.done)
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// ReloadConfig reloads the config and reinitializes the router.
func (s *Server) ReloadConfig(cfg *config.Config) {
	s.setConfig(cfg)
	s.router.Reload(cfg)
	logutil.Info("Config reloaded: %d models, %d providers", len(cfg.Models), len(cfg.Providers))
}

func (s *Server) reloadConfigFromFile() {
	if s.cfgPath == "" {
		return
	}
	cfg, err := config.Load(s.cfgPath)
	if err != nil {
		logutil.Warn("Failed to reload config: %v", err)
		return
	}
	s.ReloadConfig(cfg)
}

func (s *Server) watchConfigFile(path string) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		logutil.Warn("Config watch: cannot resolve path: %v", err)
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logutil.Warn("Config watch: cannot create watcher: %v", err)
		return
	}
	defer watcher.Close()

	dir := filepath.Dir(absPath)
	if err := watcher.Add(dir); err != nil {
		logutil.Warn("Config watch: cannot watch %s: %v", dir, err)
		return
	}

	logutil.Debug("Config watch enabled for %s", absPath)

	var debounceTimer *time.Timer

	for {
		select {
		case <-s.done:
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if filepath.Base(event.Name) == filepath.Base(absPath) {
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
					if debounceTimer != nil {
						debounceTimer.Stop()
					}
					debounceTimer = time.AfterFunc(500*time.Millisecond, func() {
						logutil.Debug("Config file changed, reloading...")
						s.reloadConfigFromFile()
					})
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			logutil.Debug("Config watch error: %v", err)
		}
	}
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	ev := &monitor.RequestEvent{
		ID:        s.tracker.NextID(),
		Timestamp: time.Now(),
		Status:    "ok",
	}

	if r.Method != http.MethodPost {
		writeAnthropicError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
		ev.Status = "error"
		ev.Error = "method not allowed"
		ev.Duration = time.Since(ev.Timestamp)
		s.tracker.Record(ev)
		return
	}

	var antReq anthropic.MessagesRequest
	limitedBody := http.MaxBytesReader(w, r.Body, 64<<20)
	if err := json.NewDecoder(limitedBody).Decode(&antReq); err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request", err.Error())
		ev.Status = "error"
		ev.Error = err.Error()
		ev.Duration = time.Since(ev.Timestamp)
		s.tracker.Record(ev)
		return
	}

	ev.Model = antReq.Model
	ev.Stream = antReq.Stream

	route, err := s.router.Route(antReq.Model)
	if err != nil {
		logutil.Error("Model not found in routing table: %q", antReq.Model)
		writeAnthropicError(w, http.StatusNotFound, "not_found",
			fmt.Sprintf("Model %q is not configured. Use `api-switch model add %s <provider>` to add it, then `api-switch use %s` to switch.", antReq.Model, antReq.Model, antReq.Model))
		ev.Status = "error"
		ev.Error = fmt.Sprintf("model %q not found", antReq.Model)
		ev.Duration = time.Since(ev.Timestamp)
		s.tracker.Record(ev)
		return
	}

	ev.Provider = route.ProviderName
	ev.ProviderType = route.ProviderType

	logutil.Info("Request model=%s provider=%s type=%s actualModel=%s stream=%v max_tokens=%d",
		antReq.Model, route.ProviderName, route.ProviderType, route.ActualModel, antReq.Stream, antReq.MaxTokens)

	// Unified call path using Provider interface with resilience
	if antReq.Stream {
		s.handleStreamingWithResilience(r.Context(), w, &antReq, route, ev)
	} else {
		s.handleNonStreamingWithResilience(r.Context(), w, &antReq, route, ev)
	}

	ev.Duration = time.Since(ev.Timestamp)
	s.tracker.Record(ev)

	// Record Prometheus metrics
	s.metricsTracker.RecordRequest(&metrics.RequestMetrics{
		Provider:     route.ProviderName,
		Model:        antReq.Model,
		Status:       ev.Status,
		Duration:     ev.Duration,
		InputTokens:  ev.InputTokens,
		OutputTokens: ev.OutputTokens,
	})

	if s.usageTracker != nil {
		s.usageTracker.RecordWithCache(ev.InputTokens, ev.OutputTokens, ev.CacheReadTokens, ev.Status == "error")
		if err := s.usageTracker.Save(); err != nil {
			logutil.Warn("Failed to save usage data: %v", err)
		}
	}
}

// closeOnCancel closes the given Closer when the context is cancelled.
func closeOnCancel(ctx context.Context, closer io.Closer) {
	context.AfterFunc(ctx, func() { _ = closer.Close() })
}

// handleNonStreamingWithResilience handles non-streaming requests with retry and circuit breaker.
func (s *Server) handleNonStreamingWithResilience(ctx context.Context, w http.ResponseWriter, antReq *anthropic.MessagesRequest, route *RouteResult, ev *monitor.RequestEvent) {
	var resp *anthropic.MessagesResponse

	err := s.orchestrator.Execute(ctx, route.Provider, route.ActualModel, route.DefaultMaxTokens, nil,
		func(p provider.Provider, model string, maxTokens int) error {
			var err error
			resp, err = p.SendMessage(ctx, antReq, model, maxTokens)
			return err
		})

	if err != nil {
		logutil.Error("Provider %s error after retries: %v", route.ProviderName, err)
		writeAnthropicError(w, http.StatusBadGateway, "api_error", err.Error())
		ev.Status = "error"
		ev.Error = err.Error()
		return
	}

	ev.InputTokens = resp.Usage.InputTokens
	ev.OutputTokens = resp.Usage.OutputTokens
	ev.CacheReadTokens = resp.Usage.CacheReadInputTokens

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logutil.Error("Response encode error: %v", err)
	}
}

// handleStreamingWithResilience handles streaming requests with retry and circuit breaker.
func (s *Server) handleStreamingWithResilience(ctx context.Context, w http.ResponseWriter, antReq *anthropic.MessagesRequest, route *RouteResult, ev *monitor.RequestEvent) {
	var respBody io.ReadCloser

	err := s.orchestrator.Execute(ctx, route.Provider, route.ActualModel, route.DefaultMaxTokens, nil,
		func(p provider.Provider, model string, maxTokens int) error {
			var err error
			respBody, err = p.StreamMessage(ctx, antReq, model, maxTokens)
			return err
		})

	if err != nil {
		logutil.Error("Provider %s streaming error after retries: %v", route.ProviderName, err)
		writeAnthropicError(w, http.StatusBadGateway, "api_error", err.Error())
		ev.Status = "error"
		ev.Error = err.Error()
		return
	}
	defer respBody.Close()

	closeOnCancel(ctx, respBody)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	flusher, canFlush := w.(http.Flusher)
	if err := streaming.AnthropicPassthroughStream(respBody, w, flusher, canFlush); err != nil {
		logutil.Error("Streaming passthrough error: %v", err)
	}
}

// writeAnthropicError writes an Anthropic-format error response.
func writeAnthropicError(w http.ResponseWriter, status int, errType, message string) {
	resp := struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}{
		Type: "error",
	}
	resp.Error.Type = errType
	resp.Error.Message = sanitizeErrorMessage(message)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logutil.Error("Failed to write error response: %v", err)
	}
}

// sanitizeErrorMessage redacts potentially sensitive information from error messages.
func sanitizeErrorMessage(msg string) string {
	if len(msg) > 500 {
		msg = msg[:500] + "...(truncated)"
	}
	return msg
}
