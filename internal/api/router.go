package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/MikeSquared-Agency/Dispatch/internal/broker"
	"github.com/MikeSquared-Agency/Dispatch/internal/config"
	"github.com/MikeSquared-Agency/Dispatch/internal/forge"
	"github.com/MikeSquared-Agency/Dispatch/internal/hermes"
	"github.com/MikeSquared-Agency/Dispatch/internal/scoring"
	"github.com/MikeSquared-Agency/Dispatch/internal/store"
	"github.com/MikeSquared-Agency/Dispatch/internal/warren"
)

func NewRouter(s store.Store, h hermes.Client, w warren.Client, f forge.Client, b *broker.Broker, bs *scoring.BacklogScorer, cfg *config.Config, adminToken string, logger *slog.Logger) http.Handler {
	r := chi.NewRouter()

	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.RequestID)
	r.Use(RequestLogger(logger))
	r.Use(RateLimitMiddleware(120))

	tasks := NewTasksHandler(s, h, cfg.ModelRouting)
	admin := NewAdminHandler(s, w, f, b)
	explain := NewExplainHandler(s)
	backlog := NewBacklogHandler(s, h, bs)
	deps := NewDependenciesHandler(s)
	overrides := NewOverridesHandler(s, h)
	autonomy := NewAutonomyHandler(s)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(AgentIDMiddleware)

		// Tasks
		r.Post("/tasks", tasks.Create)
		r.Get("/tasks", tasks.List)
		r.Get("/tasks/{id}", tasks.Get)
		r.Patch("/tasks/{id}", tasks.Update)
		r.Post("/tasks/{id}/complete", tasks.Complete)
		r.Post("/tasks/{id}/fail", tasks.Fail)
		r.Post("/tasks/{id}/progress", tasks.Progress)
		r.Patch("/tasks/{id}/discovery-complete", tasks.DiscoveryComplete)

		// Scoring
		r.Get("/scoring/explain/{task_id}", explain.Explain)

		// Backlog
		r.Post("/backlog", backlog.Create)
		r.Get("/backlog", backlog.List)
		r.Get("/backlog/next", backlog.Next)
		r.Get("/backlog/{id}", backlog.Get)
		r.Patch("/backlog/{id}", backlog.Update)
		r.Delete("/backlog/{id}", backlog.Delete)
		r.Post("/backlog/{id}/start", backlog.Start)
		r.Patch("/backlog/{id}/discovery-complete", backlog.DiscoveryComplete)
		r.Post("/backlog/{id}/begin-execution", backlog.BeginExecution)
		r.Post("/backlog/{id}/complete", backlog.Complete)
		r.Post("/backlog/{id}/block", backlog.Block)
		r.Post("/backlog/{id}/park", backlog.Park)

		// Dependencies
		r.Post("/backlog/dependencies", deps.Create)
		r.Delete("/backlog/dependencies/{id}", deps.Delete)
		r.Get("/backlog/{id}/dependencies", deps.ListForItem)

		r.Group(func(r chi.Router) {
			r.Use(AdminAuthMiddleware(adminToken))
			r.Get("/stats", admin.Stats)
			r.Get("/agents", admin.Agents)
			r.Post("/agents/{id}/drain", admin.Drain)

			// Overrides and autonomy (admin only)
			r.Post("/overrides", overrides.Create)
			r.Get("/autonomy/metrics", autonomy.Metrics)
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
