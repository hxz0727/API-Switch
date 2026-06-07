package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sync"
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
	cfg          *config.Config
	cfgPath      string
	router       *Router
	tracker      *monitor.Tracker
	usageTracker *usage.Tracker
	mu           sync.Mutex
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
		cfg:          cfg,
		router:       NewRouter(cfg),
		tracker:      monitor.NewTracker(1000),
		usageTracker: initUsageTracker(),
	}
	return s
}

// StartWithConfigFile starts the HTTP server with config file path for hot-reload.
func (s *Server) StartWithConfigFile(addr string, cfgPath string) error {
	s.cfgPath = cfgPath

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", s.handleMessages)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "ok",
			"models":    len(s.cfg.Models),
			"providers": len(s.cfg.Providers),
			"requests":  s.tracker.Stats()["total_requests"],
		})
	})
	// Admin / monitoring routes (specific routes must be registered BEFORE prefix routes)
	mux.HandleFunc("/admin/reload", s.handleAdminReload)
	mux.HandleFunc("/admin/stats", s.handleAdminStats)
	mux.HandleFunc("/admin/events", s.handleAdminEvents)
	mux.HandleFunc("/admin/", s.handleAdminDashboard)

	// Start file watcher for hot-reload
	if cfgPath != "" {
		go s.watchConfigFile(cfgPath)
	}

	return http.ListenAndServe(addr, mux)
}

// ReloadConfig reloads the config and reinitializes the router.
func (s *Server) ReloadConfig(cfg *config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
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

	ev.Model = antReq.Model
	ev.Provider = route.ProviderName
	ev.ProviderType = route.ProviderType

	logutil.Info("Request model=%s provider=%s type=%s actualModel=%s stream=%v",
		antReq.Model, route.ProviderName, route.ProviderType, route.ActualModel, antReq.Stream)

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
		_ = s.usageTracker.Save()
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
		s.handleAnthropicNonStreaming(w, antReq, route, ev)
	}
}

func (s *Server) handleAnthropicNonStreaming(w http.ResponseWriter, antReq *anthropic.MessagesRequest, route *RouteResult, ev *monitor.RequestEvent) {
	antResp, err := route.Anthropic.SendMessage(antReq)
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

	// Debug: log the raw messages and system field from Claude Code
	logutil.Debug("DEBUG Anthropic request: model=%q system=%q messages=%+v", antReq.Model, string(antReq.System), antReq.Messages)

	// Convert Anthropic request → OpenAI request
	oaiReq := ConvertAnthropicToOpenAI(antReq, route.ActualModel, defaultMaxTokens)

	// Debug: log what was converted
	for i, msg := range oaiReq.Messages {
		logutil.Debug("DEBUG OAI msg[%d]: role=%q content=%q", i, msg.Role, msg.Content[:min(len(msg.Content), 200)])
	}

	if antReq.Stream {
		s.handleOpenAIStreaming(ctx, w, oaiReq, route, antReq.Model, ev)
	} else {
		s.handleOpenAINonStreaming(w, oaiReq, route, antReq.Model, ev)
	}
}

func (s *Server) handleOpenAINonStreaming(w http.ResponseWriter, oaiReq *openai.ChatCompletionRequest, route *RouteResult, requestedModel string, ev *monitor.RequestEvent) {
	oaiResp, err := route.OpenAI.SendMessage(oaiReq)
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
	respBody, err := route.OpenAI.StreamMessage(oaiReq)
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

	flusher, canFlush := w.(http.Flusher)

	// Estimate input tokens from request
	inputTokens := estimateInputTokens(oaiReq)
	ev.InputTokens = inputTokens

	if err := streaming.OpenAIToAnthropicStream(respBody, w, flusher, canFlush, requestedModel, inputTokens); err != nil {
		logutil.Error("OpenAI→Anthropic streaming conversion error: %v", err)
	}
}

// estimateInputTokens provides a rough estimate of input tokens.
func estimateInputTokens(req *openai.ChatCompletionRequest) int {
	tokens := 0
	for _, msg := range req.Messages {
		tokens += len(msg.Content) / 4 // rough: ~4 chars per token
	}
	return tokens
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
	resp.Error.Message = message
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logutil.Error("Failed to write error response: %v", err)
	}
}
