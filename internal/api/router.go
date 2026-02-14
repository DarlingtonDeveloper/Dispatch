package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/MikeSquared-Agency/Dispatch/internal/broker"
	"github.com/MikeSquared-Agency/Dispatch/internal/forge"
	"github.com/MikeSquared-Agency/Dispatch/internal/hermes"
	"github.com/MikeSquared-Agency/Dispatch/internal/store"
	"github.com/MikeSquared-Agency/Dispatch/internal/warren"
)

func NewRouter(s store.Store, h hermes.Client, w warren.Client, f forge.Client, b *broker.Broker, adminToken string, logger *slog.Logger) http.Handler {
	r := chi.NewRouter()

	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.RequestID)
	r.Use(RequestLogger(logger))
	r.Use(RateLimitMiddleware(120))

	tasks := NewTasksHandler(s, h)
	admin := NewAdminHandler(s, w, f, b)
	explain := NewExplainHandler(s)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(AgentIDMiddleware)

		r.Post("/tasks", tasks.Create)
		r.Get("/tasks", tasks.List)
		r.Get("/tasks/{id}", tasks.Get)
		r.Patch("/tasks/{id}", tasks.Update)
		r.Post("/tasks/{id}/complete", tasks.Complete)
		r.Post("/tasks/{id}/fail", tasks.Fail)
		r.Post("/tasks/{id}/progress", tasks.Progress)

		r.Get("/scoring/explain/{task_id}", explain.Explain)

		r.Group(func(r chi.Router) {
			r.Use(AdminAuthMiddleware(adminToken))
			r.Get("/stats", admin.Stats)
			r.Get("/agents", admin.Agents)
			r.Post("/agents/{id}/drain", admin.Drain)
		})
	})

	return r
}

func NewMetricsRouter() http.Handler {
	r := chi.NewRouter()
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Handle("/metrics", promhttp.Handler())
	return r
}
