package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/user/api-switch/internal/config"
	"github.com/user/api-switch/internal/streaming"
	"github.com/user/api-switch/pkg/anthropic"
	"github.com/user/api-switch/pkg/openai"
)

// Server is the HTTP proxy server for Claude Code.
// It accepts Anthropic Messages API requests and routes them to the correct provider.
type Server struct {
	cfg    *config.Config
	router *Router
}

// NewServer creates a new proxy server.
func NewServer(cfg *config.Config) *Server {
	return &Server{
		cfg:    cfg,
		router: NewRouter(cfg),
	}
}

// Start starts the HTTP server on the given address.
func (s *Server) Start(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", s.handleMessages)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	return http.ListenAndServe(addr, mux)
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAnthropicError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
		return
	}

	// Decode Anthropic Messages request
	var antReq anthropic.MessagesRequest
	if err := json.NewDecoder(r.Body).Decode(&antReq); err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	// Route the model
	route, err := s.router.Route(antReq.Model)
	if err != nil {
		log.Printf("Model not found in routing table: %q", antReq.Model)
		writeAnthropicError(w, http.StatusNotFound, "not_found",
			fmt.Sprintf("Model %q is not configured. Use `api-switch model add %s <provider>` to add it, then `api-switch use %s` to switch.", antReq.Model, antReq.Model, antReq.Model))
		return
	}

	log.Printf("Request model=%s provider=%s type=%s actualModel=%s stream=%v",
		antReq.Model, route.ProviderName, route.ProviderType, route.ActualModel, antReq.Stream)

	switch route.ProviderType {
	case "anthropic":
		s.handleAnthropic(w, &antReq, route)
	case "openai":
		s.handleOpenAI(w, &antReq, route)
	default:
		writeAnthropicError(w, http.StatusInternalServerError, "unknown_provider_type",
			fmt.Sprintf("provider type %q is not supported", route.ProviderType))
	}
}

// handleAnthropic handles requests for Anthropic models (direct passthrough).
func (s *Server) handleAnthropic(w http.ResponseWriter, antReq *anthropic.MessagesRequest, route *RouteResult) {
	// Override model name if mapped
	antReq.Model = route.ActualModel

	if antReq.Stream {
		s.handleAnthropicStreaming(w, antReq, route)
	} else {
		s.handleAnthropicNonStreaming(w, antReq, route)
	}
}

func (s *Server) handleAnthropicNonStreaming(w http.ResponseWriter, antReq *anthropic.MessagesRequest, route *RouteResult) {
	antResp, err := route.Anthropic.SendMessage(antReq)
	if err != nil {
		log.Printf("Anthropic API error: %v", err)
		writeAnthropicError(w, http.StatusBadGateway, "api_error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(antResp)
}

func (s *Server) handleAnthropicStreaming(w http.ResponseWriter, antReq *anthropic.MessagesRequest, route *RouteResult) {
	respBody, err := route.Anthropic.StreamMessage(antReq)
	if err != nil {
		log.Printf("Anthropic streaming error: %v", err)
		writeAnthropicError(w, http.StatusBadGateway, "api_error", err.Error())
		return
	}
	defer respBody.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, canFlush := w.(http.Flusher)
	if err := streaming.AnthropicPassthroughStream(respBody, w, flusher, canFlush); err != nil {
		log.Printf("Anthropic passthrough error: %v", err)
	}
}

// handleOpenAI handles requests for OpenAI-protocol models (protocol conversion).
func (s *Server) handleOpenAI(w http.ResponseWriter, antReq *anthropic.MessagesRequest, route *RouteResult) {
	// Get default max tokens for this provider
	provCfg := s.cfg.Providers[route.ProviderName]
	defaultMaxTokens := provCfg.DefaultMaxTokens

	// Debug: log the raw messages and system field from Claude Code
	log.Printf("DEBUG Anthropic request: model=%q system=%q messages=%+v", antReq.Model, string(antReq.System), antReq.Messages)

	// Convert Anthropic request → OpenAI request
	oaiReq := ConvertAnthropicToOpenAI(antReq, route.ActualModel, defaultMaxTokens)

	// Debug: log what was converted
	for i, msg := range oaiReq.Messages {
		log.Printf("DEBUG OAI msg[%d]: role=%q content=%q", i, msg.Role, msg.Content[:min(len(msg.Content), 200)])
	}

	if antReq.Stream {
		s.handleOpenAIStreaming(w, oaiReq, route, antReq.Model)
	} else {
		s.handleOpenAINonStreaming(w, oaiReq, route, antReq.Model)
	}
}

func (s *Server) handleOpenAINonStreaming(w http.ResponseWriter, oaiReq *openai.ChatCompletionRequest, route *RouteResult, requestedModel string) {
	oaiResp, err := route.OpenAI.SendMessage(oaiReq)
	if err != nil {
		log.Printf("OpenAI API error: %v", err)
		writeAnthropicError(w, http.StatusBadGateway, "api_error", err.Error())
		return
	}

	// Convert OpenAI response → Anthropic response
	antResp := ConvertOpenAIToAnthropic(oaiResp, requestedModel)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(antResp)
}

func (s *Server) handleOpenAIStreaming(w http.ResponseWriter, oaiReq *openai.ChatCompletionRequest, route *RouteResult, requestedModel string) {
	respBody, err := route.OpenAI.StreamMessage(oaiReq)
	if err != nil {
		log.Printf("OpenAI streaming error: %v", err)
		writeAnthropicError(w, http.StatusBadGateway, "api_error", err.Error())
		return
	}
	defer respBody.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, canFlush := w.(http.Flusher)

	// Estimate input tokens from request
	inputTokens := estimateInputTokens(oaiReq)

	if err := streaming.OpenAIToAnthropicStream(respBody, w, flusher, canFlush, requestedModel, inputTokens); err != nil {
		log.Printf("OpenAI→Anthropic streaming conversion error: %v", err)
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
	json.NewEncoder(w).Encode(resp)
}
