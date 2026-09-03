package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/livepeer/discovery-service/internal/cache"
	"github.com/livepeer/discovery-service/internal/config"
	"github.com/livepeer/discovery-service/internal/db"
	"github.com/livepeer/discovery-service/internal/query"
	"github.com/livepeer/discovery-service/internal/refresh"
	"github.com/livepeer/discovery-service/internal/sources"
	"github.com/livepeer/discovery-service/pkg/discotypes"
)

const (
	headerCacheControl = "Cache-Control"
	rawCacheControl    = "public, max-age=30"
)

// Server is the HTTP API for discovery-service.
type Server struct {
	cfg     config.Config
	store   *db.Store
	refresh *refresh.Service
	query   *query.Service
	cache   *cache.Layer
	// rawRowsQuery is an optional test seam for GET /v1/discovery/raw.
	rawRowsQuery func(context.Context, []string, []string) ([]db.FlatRow, error)
}

// New builds the HTTP server dependencies.
func New(
	cfg config.Config,
	store *db.Store,
	ref *refresh.Service,
	q *query.Service,
	c *cache.Layer,
) *Server {
	return &Server{
		cfg:     cfg,
		store:   store,
		refresh: ref,
		query:   q,
		cache:   c,
	}
}

// Handler returns the root http.Handler.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/", s.redirectHome)
	r.Get("/docs", s.serveDocs)
	r.Get("/openapi.yaml", s.serveOpenAPI)

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
	serviceTypes := capabilityServiceTypes(r)
	entries, err := s.store.ListCapabilityEntries(r.Context(), serviceTypes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	caps := make([]string, 0, len(entries))
	seen := make(map[string]struct{})
	for _, e := range entries {
		if _, ok := seen[e.Capability]; ok {
			continue
		}
		seen[e.Capability] = struct{}{}
		caps = append(caps, e.Capability)
	}
	apiEntries := make([]discotypes.CapabilityEntry, 0, len(entries))
	for _, e := range entries {
		apiEntries = append(apiEntries, discotypes.CapabilityEntry{
			ServiceType: e.ServiceType,
			Capability:  e.Capability,
			OfferingIDs: e.OfferingIDs,
		})
	}
	w.Header().Set(headerCacheControl, "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{
		"capabilities": caps,
		"entries":      apiEntries,
	})
}

func (s *Server) discoveryQuery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(headerCacheControl, "no-store, private")
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
	serviceTypes := capabilityServiceTypes(r)
	caps := rawCapabilityFilter(r)

	if s.cache != nil {
		if cached, ok := s.cache.GetRaw(r.Context(), caps, serviceTypes); ok {
			w.Header().Set(headerCacheControl, rawCacheControl)
			writeJSON(w, http.StatusOK, cached)
			return
		}
	}

	rows, err := s.loadRawRows(r.Context(), caps, serviceTypes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := webhookOrchestratorsFromRaw(groupRawOrchestrators(rows))
	if s.cache != nil {
		s.cache.SetRaw(r.Context(), caps, serviceTypes, out)
	}
	w.Header().Set(headerCacheControl, rawCacheControl)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) loadRawRows(ctx context.Context, capabilities []string, serviceTypes []string) ([]db.FlatRow, error) {
	if s.rawRowsQuery != nil {
		return s.rawRowsQuery(ctx, capabilities, serviceTypes)
	}
	return s.store.QueryRawRows(ctx, capabilities, serviceTypes)
}

type rawOrchEntry struct {
	address string
	score   float32
	caps    []string
}

func capabilityServiceTypes(r *http.Request) []string {
	raw := r.URL.Query()["serviceType"]
	if len(raw) == 0 {
		raw = r.URL.Query()["serviceTypes"]
	}
	types := sources.ParseServiceTypes(raw)
	out := make([]string, 0, len(types))
	for _, t := range types {
		out = append(out, string(t))
	}
	return out
}

func rawCapabilityFilter(r *http.Request) []string {
	caps := r.URL.Query()["caps"]
	if len(caps) == 0 {
		caps = r.URL.Query()["capability"]
	}
	if len(caps) == 0 {
		return nil
	}
	return normalizeLegacyCaps(caps, capabilityServiceTypes(r))
}

// normalizeLegacyCaps expands incoming capability filters for live-video,
// live-runner, and batch rows. For each cap it keeps the exact string
// (live-runner apps like "transcode/ffmpeg") and also the bare model name after
// stripping a known pipeline prefix. Modules-only queries leave opaque IDs
// untouched.
func normalizeLegacyCaps(caps []string, serviceTypes []string) []string {
	if !includesPipelineServiceType(serviceTypes) {
		return caps
	}
	out := make([]string, 0, len(caps)*2)
	seen := make(map[string]struct{}, len(caps)*2)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	for _, c := range caps {
		add(c)
		stripped := sources.ExtractCapabilityName(c)
		if stripped != strings.TrimSpace(c) {
			add(stripped)
		}
	}
	return out
}

func includesPipelineServiceType(serviceTypes []string) bool {
	for _, st := range sources.ParseServiceTypes(serviceTypes) {
		switch st {
		case sources.ServiceTypeLiveVideoToVideo, sources.ServiceTypeLiveRunner, sources.ServiceTypeBatch:
			return true
		}
	}
	return false
}

func groupRawOrchestrators(rows []db.FlatRow) map[string]*rawOrchEntry {
	byAddr := make(map[string]*rawOrchEntry, len(rows))
	for _, row := range rows {
		recordRawOrch(byAddr, row, row.Capability)
	}
	return byAddr
}

func recordRawOrch(byAddr map[string]*rawOrchEntry, row db.FlatRow, cap string) {
	e, ok := byAddr[row.OrchURI]
	if !ok {
		score := float32(row.Score)
		if score == 0 {
			score = 1
		}
		e = &rawOrchEntry{address: row.OrchURI, score: score}
		byAddr[row.OrchURI] = e
	}
	e.caps = appendUnique(e.caps, cap)
}

func webhookOrchestratorsFromRaw(byAddr map[string]*rawOrchEntry) []discotypes.WebhookOrchestrator {
	out := make([]discotypes.WebhookOrchestrator, 0, len(byAddr))
	for _, e := range byAddr {
		out = append(out, discotypes.WebhookOrchestrator{
			Address:      e.address,
			Score:        e.score,
			Capabilities: e.caps,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Address < out[j].Address
	})
	return out
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
	w.Header().Set(headerCacheControl, "no-store, private")
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
