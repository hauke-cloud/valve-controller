package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewRouter builds the chi router with all routes mounted.
// gatherer is the Prometheus registry used for the /metrics endpoint;
// pass the controller-runtime registry so the same set of metrics is served.
func NewRouter(h *Handler, gatherer prometheus.Gatherer) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// Probe and metrics endpoints — no client cert required.
	r.Get("/api/v1/healthz", h.Healthz)
	r.Get("/api/v1/readyz", h.Readyz)
	r.Handle("/metrics", promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	}))

	// Protected routes require a verified client certificate.
	r.Group(func(r chi.Router) {
		r.Use(requireClientCert)

		r.Get("/api/v1/valves", h.ListValves)
		r.Get("/api/v1/valves/{name}", h.GetValve)
		r.Put("/api/v1/valves/{name}/open", h.OpenValve)
		r.Put("/api/v1/valves/{name}/close", h.CloseValve)
		r.Get("/api/v1/valves/{name}/actions", h.ListActions)
		r.Get("/api/v1/valves/{name}/actions/{actionID}", h.GetAction)
	})

	return r
}
