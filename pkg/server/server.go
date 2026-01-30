package server

import (
	"net"
	"net/http"
	"strings"

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
	logger    *logging.Logger
}

// New creates a server instance.
func New(cfg config.Config, allowlist allowlist.Allowlist, queue *queue.Queue) *Server {
	return &Server{cfg: cfg, allowlist: allowlist, queue: queue, logger: logging.Default()}
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(s.cfg.WebhookPath, s.handleWebhook)
	return mux
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
