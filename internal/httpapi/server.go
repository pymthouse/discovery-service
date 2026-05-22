package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/livepeer/discovery-service/internal/cache"
	"github.com/livepeer/discovery-service/internal/config"
	"github.com/livepeer/discovery-service/internal/db"
	"github.com/livepeer/discovery-service/internal/query"
	"github.com/livepeer/discovery-service/internal/refresh"
	"github.com/livepeer/discovery-service/pkg/discotypes"
)

// Server is the HTTP API for discovery-service.
type Server struct {
	cfg     config.Config
	store   *db.Store
	refresh *refresh.Service
	query   *query.Service
	cache   *cache.Layer
}

// New builds the HTTP server dependencies.
func New(
	cfg config.Config,
	store *db.Store,
	ref *refresh.Service,
	q *query.Service,
	c *cache.Layer,
) *Server {
	return &Server{cfg: cfg, store: store, refresh: ref, query: q, cache: c}
}

// Handler returns the root http.Handler.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", s.healthz)
	r.Get("/v1/discovery/health", s.healthz)
	r.Get("/v1/discovery/freshness", s.freshness)

	r.Route("/v1/discovery", func(r chi.Router) {
		r.Get("/capabilities", s.capabilities)
		r.Post("/query", s.discoveryQuery)
		r.Get("/raw", s.discoveryRaw)
		r.Post("/dataset/refresh", s.datasetRefresh)
	})

	return r
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) freshness(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.GetStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var refreshedAt *int64
	var ageMs int64
	if stats.RefreshedAt != nil {
		ms := stats.RefreshedAt.UnixMilli()
		refreshedAt = &ms
		ageMs = time.Since(*stats.RefreshedAt).Milliseconds()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"populated":       stats.Populated,
		"refreshedAt":     refreshedAt,
		"refreshedBy":     stats.RefreshedBy,
		"ageMs":           ageMs,
		"totalRows":       stats.TotalRows,
		"capabilityCount": stats.CapabilityCount,
	})
}

func (s *Server) capabilities(w http.ResponseWriter, r *http.Request) {
	caps, err := s.store.ListCapabilities(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"capabilities": caps})
}

func (s *Server) discoveryQuery(w http.ResponseWriter, r *http.Request) {
	var req discotypes.QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(req.Capabilities) == 0 {
		writeError(w, http.StatusBadRequest, "capabilities is required")
		return
	}

	t0 := time.Now()
	if s.cache != nil {
		if cached, ok := s.cache.Get(r.Context(), req); ok {
			cached.QueryTimeMs = time.Since(t0).Milliseconds()
			writeJSON(w, http.StatusOK, cached)
			return
		}
	}

	resp, err := s.query.Execute(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp.QueryTimeMs = time.Since(t0).Milliseconds()
	if meta, err := s.store.GetConfig(r.Context()); err == nil && meta.LastRefreshedAt != nil {
		resp.DatasetVersion = meta.DatasetVersion
		ms := meta.LastRefreshedAt.UnixMilli()
		age := time.Since(*meta.LastRefreshedAt).Milliseconds()
		resp.SourceFreshness = &discotypes.FreshnessMeta{
			RefreshedAt:     &ms,
			RefreshedBy:     meta.LastRefreshedBy,
			AgeMs:           age,
			CapabilityCount: len(meta.KnownCapabilities),
		}
	}
	if s.cache != nil {
		s.cache.Set(r.Context(), req, resp)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) discoveryRaw(w http.ResponseWriter, r *http.Request) {
	caps := r.URL.Query()["caps"]
	if len(caps) == 0 {
		caps = r.URL.Query()["capability"]
	}
	if len(caps) == 0 {
		all, err := s.store.ListCapabilities(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		caps = all
	}

	type orchEntry struct {
		address string
		score   float32
		caps    []string
	}
	byAddr := make(map[string]*orchEntry)

	for _, cap := range caps {
		rows, err := s.store.QueryRows(r.Context(), cap, db.QueryFilters{}, 1000)
		if err != nil {
			continue
		}
		for _, row := range rows {
			e, ok := byAddr[row.OrchURI]
			if !ok {
				e = &orchEntry{address: row.OrchURI, score: float32(row.Score)}
				if e.score == 0 {
					e.score = 1
				}
				byAddr[row.OrchURI] = e
			}
			e.caps = appendUnique(e.caps, cap)
		}
	}

	out := make([]discotypes.WebhookOrchestrator, 0, len(byAddr))
	for _, e := range byAddr {
		out = append(out, discotypes.WebhookOrchestrator{
			Address:      e.address,
			Score:        e.score,
			Capabilities: e.caps,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func appendUnique(slice []string, v string) []string {
	for _, s := range slice {
		if s == v {
			return slice
		}
	}
	return append(slice, v)
}

func (s *Server) datasetRefresh(w http.ResponseWriter, r *http.Request) {
	if s.cfg.CronSecret != "" {
		auth := r.Header.Get("Authorization")
		secret := r.Header.Get("X-Cron-Secret")
		ok := strings.TrimPrefix(auth, "Bearer ") == s.cfg.CronSecret || secret == s.cfg.CronSecret
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
	}
	refreshedBy := r.Header.Get("X-Refreshed-By")
	if refreshedBy == "" {
		refreshedBy = "api"
	}
	result, err := s.refresh.Run(r.Context(), refreshedBy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.cache != nil {
		s.cache.InvalidateAll()
	}
	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ListenAndServe starts the HTTP server.
func ListenAndServe(ctx context.Context, addr string, handler http.Handler) error {
	srv := &http.Server{Addr: addr, Handler: handler}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
