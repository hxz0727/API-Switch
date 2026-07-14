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
	"github.com/hxz0727/API-Switch/internal/monitor"
	"github.com/hxz0727/API-Switch/internal/streaming"
	"github.com/hxz0727/API-Switch/internal/usage"
	"github.com/hxz0727/API-Switch/pkg/anthropic"
	"github.com/hxz0727/API-Switch/pkg/openai"
)

// Server is the HTTP proxy server for Claude Code.
// It accepts Anthropic Messages API requests and routes them to the correct provider.
type Server struct {
	cfg          atomic.Value // *config.Config — safe for concurrent read
	cfgPath      string
	router       *Router
	tracker      *monitor.Tracker
	usageTracker *usage.Tracker
	mu           sync.Mutex
	httpServer   *http.Server
	done         chan struct{}
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
		router:       NewRouter(cfg),
		tracker:      monitor.NewTracker(1000),
		usageTracker: initUsageTracker(),
		done:         make(chan struct{}),
	}
	s.setConfig(cfg)
	return s
}

// authMiddleware checks for valid Bearer token if auth is configured.
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Skip auth if no token configured
		cfg := s.getConfig()
		if cfg.Server.AuthToken == "" {
			next(w, r)
			return
		}

		// Check Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeAnthropicError(w, http.StatusUnauthorized, "authentication_error",
				"Missing Authorization header. Use: Authorization: Bearer <token>")
			return
		}

		// Parse Bearer token
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
	// Apply auth and rate limit middleware to the main API endpoint
	handler := s.rateLimitMiddleware(s.authMiddleware(s.handleMessages))
	mux.HandleFunc("/v1/messages", handler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		cfg := s.getConfig()
		numModels := len(cfg.Models)
		numProviders := len(cfg.Providers)
		reqStats := s.tracker.Stats()["total_requests"]
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "ok",
			"models":    numModels,
			"providers": numProviders,
			"requests":  reqStats,
		})
	})
	// Admin / monitoring routes (specific routes must be registered BEFORE prefix routes)
	// All admin endpoints are restricted to localhost for security.
	mux.HandleFunc("/admin/reload", requireLocalhost(s.handleAdminReload))
	mux.HandleFunc("/admin/stats", requireLocalhost(s.handleAdminStats))
	mux.HandleFunc("/admin/events", requireLocalhost(s.handleAdminEvents))
	mux.HandleFunc("/admin/", requireLocalhost(s.handleAdminDashboard))

	// Start file watcher for hot-reload
	if cfgPath != "" {
		go s.watchConfigFile(cfgPath)
	}

	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      0,  // 0 = no timeout, required for SSE streaming
		IdleTimeout:       120 * time.Second,
	}

	// Check if TLS is configured
	cfg := s.getConfig()
	if cfg.Server.TLSCert != "" && cfg.Server.TLSKey != "" {
		logutil.Info("TLS enabled: cert=%s, key=%s", cfg.Server.TLSCert, cfg.Server.TLSKey)
		return s.httpServer.ListenAndServeTLS(cfg.Server.TLSCert, cfg.Server.TLSKey)
	}

	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server and cleans up resources.
func (s *Server) Shutdown(ctx context.Context) error {
	// Signal the config watcher to stop
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

// reloadConfigFromFile reloads the config from disk.
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

// watchConfigFile watches the config file for changes and auto-reloads.
func (s *Server) watchConfigFile(path string) {
	// Resolve symlinks or use the actual path
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

	// Watch the directory containing the config file
	// (some editors write a temp file then rename it)
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
			// Check if the event is for our config file
			if filepath.Base(event.Name) == filepath.Base(absPath) {
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
					// Debounce: wait 500ms after last change before reloading
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

	// Decode Anthropic Messages request (limit body to 64MB)
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

	// Route the model
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

	switch route.ProviderType {
	case "anthropic":
		s.handleAnthropic(r.Context(), w, &antReq, route, ev)
	case "openai":
		s.handleOpenAI(r.Context(), w, &antReq, route, ev)
	default:
		writeAnthropicError(w, http.StatusInternalServerError, "unknown_provider_type",
			fmt.Sprintf("provider type %q is not supported", route.ProviderType))
		ev.Status = "error"
		ev.Error = "unknown provider type"
	}

	ev.Duration = time.Since(ev.Timestamp)
	s.tracker.Record(ev)
	if s.usageTracker != nil {
		s.usageTracker.RecordWithCache(ev.InputTokens, ev.OutputTokens, ev.CacheReadTokens, ev.Status == "error")
		if err := s.usageTracker.Save(); err != nil {
			logutil.Warn("Failed to save usage data: %v", err)
		}
	}
}

// closeOnCancel closes the given Closer when the context is cancelled.
// Used to abort upstream reads when the client disconnects.
func closeOnCancel(ctx context.Context, closer io.Closer) {
	go func() {
		<-ctx.Done()
		closer.Close()
	}()
}

// handleAnthropic handles requests for Anthropic models (direct passthrough).
func (s *Server) handleAnthropic(ctx context.Context, w http.ResponseWriter, antReq *anthropic.MessagesRequest, route *RouteResult, ev *monitor.RequestEvent) {
	// Override model name if mapped
	antReq.Model = route.ActualModel

	if antReq.Stream {
		s.handleAnthropicStreaming(ctx, w, antReq, route, ev)
	} else {
		s.handleAnthropicNonStreaming(ctx, w, antReq, route, ev)
	}
}

func (s *Server) handleAnthropicNonStreaming(ctx context.Context, w http.ResponseWriter, antReq *anthropic.MessagesRequest, route *RouteResult, ev *monitor.RequestEvent) {
	antResp, err := route.Anthropic.SendMessageWithContext(ctx, antReq)
	if err != nil {
		logutil.Error("Anthropic API error: %v", err)
		writeAnthropicError(w, http.StatusBadGateway, "api_error", err.Error())
		ev.Status = "error"
		ev.Error = err.Error()
		return
	}

	ev.InputTokens = antResp.Usage.InputTokens
	ev.OutputTokens = antResp.Usage.OutputTokens
	ev.CacheReadTokens = antResp.Usage.CacheReadInputTokens

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(antResp); err != nil {
		logutil.Error("Anthropic response encode error: %v", err)
	}
}

func (s *Server) handleAnthropicStreaming(ctx context.Context, w http.ResponseWriter, antReq *anthropic.MessagesRequest, route *RouteResult, ev *monitor.RequestEvent) {
	respBody, err := route.Anthropic.StreamMessage(antReq)
	if err != nil {
		logutil.Error("Anthropic streaming error: %v", err)
		writeAnthropicError(w, http.StatusBadGateway, "api_error", err.Error())
		ev.Status = "error"
		ev.Error = err.Error()
		return
	}
	defer respBody.Close()

	// Close upstream body when client disconnects
	closeOnCancel(ctx, respBody)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, canFlush := w.(http.Flusher)
	if err := streaming.AnthropicPassthroughStream(respBody, w, flusher, canFlush); err != nil {
		logutil.Error("Anthropic passthrough error: %v", err)
	}
}

// handleOpenAI handles requests for OpenAI-protocol models (protocol conversion).
func (s *Server) handleOpenAI(ctx context.Context, w http.ResponseWriter, antReq *anthropic.MessagesRequest, route *RouteResult, ev *monitor.RequestEvent) {
	// Get default max tokens from route (populated by router under lock)
	defaultMaxTokens := route.DefaultMaxTokens

	// Convert Anthropic request → OpenAI request
	oaiReq := ConvertAnthropicToOpenAI(antReq, route.ActualModel, defaultMaxTokens)

	// Debug: log what was converted
	for i, msg := range oaiReq.Messages {
		contentStr := oaiContentToString(msg.Content)
		if len(contentStr) > 200 {
			contentStr = contentStr[:200]
		}
		logutil.Debug("DEBUG OAI msg[%d]: role=%q content=%q", i, msg.Role, contentStr)
	}

	if antReq.Stream {
		s.handleOpenAIStreaming(ctx, w, oaiReq, route, antReq.Model, ev)
	} else {
		s.handleOpenAINonStreaming(ctx, w, oaiReq, route, antReq.Model, ev)
	}
}

func (s *Server) handleOpenAINonStreaming(ctx context.Context, w http.ResponseWriter, oaiReq *openai.ChatCompletionRequest, route *RouteResult, requestedModel string, ev *monitor.RequestEvent) {
	oaiResp, err := route.OpenAI.SendMessageWithContext(ctx, oaiReq)
	if err != nil {
		logutil.Error("OpenAI API error: %v", err)
		writeAnthropicError(w, http.StatusBadGateway, "api_error", err.Error())
		ev.Status = "error"
		ev.Error = err.Error()
		return
	}

	ev.InputTokens = oaiResp.Usage.PromptTokens
	ev.OutputTokens = oaiResp.Usage.CompletionTokens

	// Convert OpenAI response → Anthropic response
	antResp := ConvertOpenAIToAnthropic(oaiResp, requestedModel)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(antResp); err != nil {
		logutil.Error("OpenAI response encode error: %v", err)
	}
}

func (s *Server) handleOpenAIStreaming(ctx context.Context, w http.ResponseWriter, oaiReq *openai.ChatCompletionRequest, route *RouteResult, requestedModel string, ev *monitor.RequestEvent) {
	respBody, err := route.OpenAI.StreamMessageWithContext(ctx, oaiReq)
	if err != nil {
		logutil.Error("OpenAI streaming error: %v", err)
		writeAnthropicError(w, http.StatusBadGateway, "api_error", err.Error())
		ev.Status = "error"
		ev.Error = err.Error()
		return
	}
	defer respBody.Close()

	// Close upstream body when client disconnects
	closeOnCancel(ctx, respBody)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	flusher, canFlush := w.(http.Flusher)

	// Estimate input tokens from request
	inputTokens := estimateInputTokens(oaiReq)
	ev.InputTokens = inputTokens

	if err := streaming.OpenAIToAnthropicStream(respBody, w, flusher, canFlush, requestedModel, inputTokens); err != nil {
		logutil.Error("OpenAI→Anthropic streaming conversion error: %v", err)
	}
}

// estimateInputTokens provides a rough estimate of input tokens.
// Uses a simple character-based heuristic (~4 chars per token) since
// we don't have access to the upstream provider's tokenizer.
// Actual token counts may vary significantly depending on the model.
func estimateInputTokens(req *openai.ChatCompletionRequest) int {
	tokens := 0
	for _, msg := range req.Messages {
		contentStr := oaiContentToString(msg.Content)
		tokens += len(contentStr) / 4 // rough: ~4 chars per token
	}
	return tokens
}

// oaiContentToString extracts a string from OpenAI message content (string or []ContentPart).
func oaiContentToString(content interface{}) string {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return v
	case []openai.ContentPart:
		var texts []string
		for _, part := range v {
			if part.Type == "text" {
				texts = append(texts, part.Text)
			}
		}
		return strings.Join(texts, "\n")
	default:
		return fmt.Sprintf("%v", content)
	}
}

// writeAnthropicError writes an Anthropic-format error response.
// The message is sanitized to prevent leaking internal details.
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
	// Truncate to 500 chars
	if len(msg) > 500 {
		msg = msg[:500] + "...(truncated)"
	}
	return msg
}
