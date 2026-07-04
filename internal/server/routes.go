package server

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/lovelytoaster94/routerhub/internal/admin"
	"github.com/lovelytoaster94/routerhub/internal/config"
	"github.com/lovelytoaster94/routerhub/internal/gateway"
	"github.com/lovelytoaster94/routerhub/internal/webui"
)

// New creates and returns a new chi router with all routes configured.
func New(db *sql.DB, cfg *config.Config) http.Handler {
	r := chi.NewRouter()

	// Middleware
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Heartbeat("/ping"))

	// Admin handler
	adminHandler := admin.NewAdminHandler(db)

	// Gateway handler
	gatewayHandler := gateway.NewGatewayHandler(db)

	// Admin API routes (no auth required)
	r.Route("/api", func(r chi.Router) {
		// Health
		r.Get("/health", adminHandler.Health)
		r.Get("/system/info", adminHandler.SystemInfo)

		// Setup
		r.Get("/setup/status", adminHandler.SetupStatus)
		r.Post("/setup/init", adminHandler.SetupInit)

		// Login (no auth)
		r.Post("/auth/login", adminHandler.Login)

		// Admin routes (auth required)
		r.Group(func(r chi.Router) {
			r.Use(admin.AdminAuthMiddleware(db))

			r.Post("/auth/logout", adminHandler.Logout)
			r.Get("/auth/me", adminHandler.GetMe)
			r.Put("/auth/me", adminHandler.UpdateMe)

			// Providers CRUD
			r.Get("/providers", adminHandler.ListProviders)
			r.Get("/providers/{id}", adminHandler.GetProvider)
			r.Post("/providers", adminHandler.CreateProvider)
			r.Put("/providers/{id}", adminHandler.UpdateProvider)
			r.Delete("/providers/{id}", adminHandler.DeleteProvider)

			// Model Aliases CRUD
			r.Get("/model-aliases", adminHandler.ListModelAliases)
			r.Get("/model-aliases/{id}", adminHandler.GetModelAlias)
			r.Post("/model-aliases", adminHandler.CreateModelAlias)
			r.Put("/model-aliases/{id}", adminHandler.UpdateModelAlias)
			r.Delete("/model-aliases/{id}", adminHandler.DeleteModelAlias)

			// Gateway API Keys CRUD
			r.Get("/gateway-keys", adminHandler.ListGatewayAPIKeys)
			r.Get("/gateway-keys/{id}", adminHandler.GetGatewayAPIKey)
			r.Post("/gateway-keys", adminHandler.CreateGatewayAPIKey)
			r.Put("/gateway-keys/{id}", adminHandler.UpdateGatewayAPIKey)
			r.Delete("/gateway-keys/{id}", adminHandler.DeleteGatewayAPIKey)

			// Settings
			r.Get("/settings", adminHandler.GetSettings)
			r.Put("/settings", adminHandler.UpdateSettings)

			// Stats
			r.Get("/stats/summary", adminHandler.StatsSummary)
			r.Get("/stats/summary/stream", adminHandler.StatsSummaryStream)

			// Request Logs
			r.Get("/request-logs", adminHandler.ListRequestLogs)

			// Provider Models CRUD
			r.Get("/providers/{id}/models", adminHandler.ListProviderModels)
			r.Post("/providers/{id}/models", adminHandler.CreateProviderModel)
			r.Put("/providers/{id}/models/{modelId}", adminHandler.UpdateProviderModel)
			r.Delete("/providers/{id}/models/{modelId}", adminHandler.DeleteProviderModel)

			// Provider Models Import
			r.Post("/providers/{id}/models/import", adminHandler.ImportProviderModels)

			// Provider Fetch Models (upstream, not persisted)
			r.Post("/providers/{id}/models/fetch", adminHandler.FetchProviderModels)

			// Provider Fetch Models Preview (for unsaved providers)
			r.Post("/providers/models/preview", adminHandler.FetchModelsPreview)

			// Provider Model Aliases (scoped to provider)
			r.Get("/providers/{id}/model-aliases", adminHandler.ListProviderModelAliases)
		})
	})

	// Gateway API routes (LLM endpoints with gateway auth)
	r.Group(func(r chi.Router) {
		r.Use(gateway.GatewayAuthMiddleware(db))

		// OpenAI Chat Completions
		r.Post("/v1/chat/completions", gatewayHandler.ServeHTTP)
		// OpenAI Responses
		r.Post("/v1/responses", gatewayHandler.ServeHTTP)
		// Anthropic Messages
		r.Post("/v1/messages", gatewayHandler.ServeHTTP)
	})

	// Static frontend: all other paths serve the SPA
	r.NotFound(webui.StaticHandler().ServeHTTP)

	return r
}
