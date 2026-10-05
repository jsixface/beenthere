// Package api implements the /api/v1 JSON contract of the Dawarich Rails app.
package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/sixface/beenthere/internal/httpx"
	"github.com/sixface/beenthere/internal/jobs"
	"github.com/sixface/beenthere/internal/store"
)

type Server struct {
	S    *store.Store
	Jobs *jobs.Manager
	Log  *slog.Logger
	// MaxUploadBytes bounds multipart import uploads.
	MaxUploadBytes int64
}

type userHandler func(w http.ResponseWriter, r *http.Request, u *store.User)

type ctxKey struct{}

// auth authenticates by API key. When active is true the account must also be
// active (matches authenticate_active_api_user!).
func (s *Server) auth(active bool, h userHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Dawarich-Version", httpx.Version)
		u, err := s.S.UserByAPIKey(r.Context(), httpx.APIKey(r))
		if err != nil {
			w.Header().Set("X-Dawarich-Response", "Hey, I'm alive!")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("X-Dawarich-Response", "Hey, I'm alive and authenticated!")
		if u.Status == 3 {
			httpx.JSON(w, http.StatusPaymentRequired, map[string]string{"error": "payment_required"})
			return
		}
		if active && !u.Active() {
			httpx.Error(w, http.StatusUnauthorized, "User account is not active")
			return
		}
		h(w, r, u)
	}
}

// Routes registers all API routes on mux.
func (s *Server) Routes(mux *http.ServeMux) {
	p := "/api/v1"
	mux.HandleFunc("GET "+p+"/health", s.health)
	mux.HandleFunc("GET "+p+"/ready", s.ready)

	mux.HandleFunc("GET "+p+"/users/me", s.auth(false, s.usersMe))
	mux.HandleFunc("GET "+p+"/settings", s.auth(false, s.settingsIndex))
	mux.HandleFunc("PATCH "+p+"/settings", s.auth(true, s.settingsUpdate))

	mux.HandleFunc("GET "+p+"/points", s.auth(false, s.pointsIndex))
	mux.HandleFunc("POST "+p+"/points", s.auth(true, s.pointsCreate))
	mux.HandleFunc("PATCH "+p+"/points/{id}", s.auth(true, s.pointsUpdate))
	mux.HandleFunc("PUT "+p+"/points/{id}", s.auth(true, s.pointsUpdate))
	mux.HandleFunc("DELETE "+p+"/points/{id}", s.auth(true, s.pointsDestroy))
	mux.HandleFunc("DELETE "+p+"/points/bulk_destroy", s.auth(true, s.pointsBulkDestroy))
	mux.HandleFunc("GET "+p+"/points/tracked_months", s.auth(false, s.trackedMonths))

	mux.HandleFunc("POST "+p+"/overland/batches", s.auth(true, s.overland))
	mux.HandleFunc("POST "+p+"/owntracks/points", s.auth(true, s.owntracks))
	mux.HandleFunc("POST "+p+"/traccar/points", s.auth(true, s.traccar))

	mux.HandleFunc("GET "+p+"/areas", s.auth(false, s.areasIndex))
	mux.HandleFunc("GET "+p+"/areas/{id}", s.auth(false, s.areasShow))
	mux.HandleFunc("POST "+p+"/areas", s.auth(false, s.areasCreate))
	mux.HandleFunc("PATCH "+p+"/areas/{id}", s.auth(false, s.areasUpdate))
	mux.HandleFunc("PUT "+p+"/areas/{id}", s.auth(false, s.areasUpdate))
	mux.HandleFunc("DELETE "+p+"/areas/{id}", s.auth(false, s.areasDestroy))

	mux.HandleFunc("GET "+p+"/stats", s.auth(false, s.statsIndex))
	mux.HandleFunc("GET "+p+"/tracks", s.auth(false, s.tracksIndex))
	mux.HandleFunc("GET "+p+"/tracks/{id}", s.auth(false, s.tracksShow))
	mux.HandleFunc("GET "+p+"/tracks/{id}/points", s.auth(false, s.tracksPoints))

	s.visitRoutes(mux)
	s.placeRoutes(mux)
	s.noteTagRoutes(mux)
	s.tripRoutes(mux)
	s.importExportRoutes(mux)
	s.tileRoutes(mux)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Dawarich-Version", httpx.Version)
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2e9)
	defer cancel()
	if err := s.S.Pool.Ping(ctx); err != nil {
		httpx.JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
