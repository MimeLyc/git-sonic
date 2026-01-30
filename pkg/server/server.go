package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"

	"git_sonic/pkg/agent"
	"git_sonic/pkg/allowlist"
	"git_sonic/pkg/config"
	"git_sonic/pkg/logging"
	"git_sonic/pkg/queue"
	"git_sonic/pkg/webhook"
)

// Server handles webhook requests.
type Server struct {
	cfg       config.Config
	allowlist allowlist.Allowlist
	queue     *queue.Queue
	agent     agent.Agent
	logger    *logging.Logger
}

// New creates a server instance.
func New(cfg config.Config, allowlist allowlist.Allowlist, queue *queue.Queue) *Server {
	return &Server{cfg: cfg, allowlist: allowlist, queue: queue, logger: logging.Default()}
}

// WithAgent sets the agent for chat endpoint.
func (s *Server) WithAgent(ag agent.Agent) *Server {
	s.agent = ag
	return s
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(s.cfg.WebhookPath, s.handleWebhook)
	mux.HandleFunc("/chat", s.handleChat)
	mux.HandleFunc("/healthz", s.handleHealthz)
	return mux
}

// ChatRequest is the request body for /chat endpoint.
type ChatRequest struct {
	Message      string `json:"message"`
	SystemPrompt string `json:"system_prompt,omitempty"`
}

// ChatResponse is the response body for /chat endpoint.
type ChatResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Summary string `json:"summary,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	log := s.logger.With("path", r.URL.Path)

	if r.Method != http.MethodPost {
		log.Warn("chat rejected: invalid method", "method", r.Method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if s.agent == nil {
		log.Error("chat rejected: agent not configured")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ChatResponse{
			Success: false,
			Error:   "agent not configured",
		})
		return
	}

	// Parse request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Error("chat read body error", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req ChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Error("chat parse error", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ChatResponse{
			Success: false,
			Error:   "invalid JSON request",
		})
		return
	}

	if req.Message == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ChatResponse{
			Success: false,
			Error:   "message is required",
		})
		return
	}

	log.Info("chat request received", "message_length", len(req.Message))

	// Build agent request
	agentReq := agent.AgentRequest{
		Task:         req.Message,
		SystemPrompt: req.SystemPrompt,
	}

	if agentReq.SystemPrompt == "" {
		agentReq.SystemPrompt = "You are a helpful assistant. Answer the user's question concisely."
	}

	// Execute agent
	ctx := r.Context()
	result, err := s.agent.Execute(ctx, agentReq)
	if err != nil {
		log.Error("chat agent error", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ChatResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	log.Info("chat response sent", "success", result.Success)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ChatResponse{
		Success: result.Success,
		Message: result.Message,
		Summary: result.Summary,
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	log := s.logger.With("path", r.URL.Path)

	if r.Method != http.MethodPost {
		log.Warn("webhook rejected: invalid method", "method", r.Method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	clientIP := extractClientIP(r)
	if !s.allowlist.Allows(clientIP) {
		log.Warn("webhook rejected: IP not in allowlist", "client_ip", clientIP.String())
		w.WriteHeader(http.StatusForbidden)
		return
	}
	event, err := webhook.ParseEvent(r)
	if err != nil {
		log.Error("webhook parse error", "client_ip", clientIP.String(), "error", err)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid webhook payload"))
		return
	}

	log = log.With(
		"delivery_id", event.DeliveryID,
		"event_type", event.Type,
		"action", event.Action,
		"repo", event.Repository.FullName,
	)

	if err := s.queue.Enqueue(queue.Job{Event: event}); err != nil {
		log.Error("webhook enqueue failed", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	log.Info("webhook accepted")
	w.WriteHeader(http.StatusAccepted)
}

func extractClientIP(r *http.Request) net.IP {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if parsed := net.ParseIP(ip); parsed != nil {
				return parsed
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return net.ParseIP(host)
	}
	return net.ParseIP(r.RemoteAddr)
}

// Ensure context is used (for linter)
var _ context.Context
