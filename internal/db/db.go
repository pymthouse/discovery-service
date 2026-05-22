package db

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations.sql
var migrationFS embed.FS

// Store provides Postgres access for the discovery dataset.
type Store struct {
	pool *pgxpool.Pool
}

// New opens a connection pool and runs migrations.
func New(ctx context.Context, databaseURL string) (*Store, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	s := &Store{pool: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	sql, err := migrationFS.ReadFile("migrations.sql")
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, string(sql))
	return err
}

// Close closes the pool.
func (s *Store) Close() {
	s.pool.Close()
}

// SourceRow is a leaderboard_sources record.
type SourceRow struct {
	Kind     string
	Priority int
	Enabled  bool
}

// LoadSources returns source configuration ordered by priority.
func (s *Store) LoadSources(ctx context.Context) ([]SourceRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT kind, priority, enabled FROM leaderboard_sources ORDER BY priority ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceRow
	for rows.Next() {
		var r SourceRow
		if err := rows.Scan(&r.Kind, &r.Priority, &r.Enabled); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ConfigMeta holds singleton leaderboard config.
type ConfigMeta struct {
	LastRefreshedAt    *time.Time
	LastRefreshedBy    string
	KnownCapabilities  []string
	MembershipStrategy string
	RefreshIntervalMs  int64
	DatasetVersion     int64
}

// GetConfig loads singleton config metadata.
func (s *Store) GetConfig(ctx context.Context) (ConfigMeta, error) {
	var meta ConfigMeta
	var capsJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT last_refreshed_at, COALESCE(last_refreshed_by, ''),
			known_capabilities, membership_strategy, refresh_interval_ms
		FROM leaderboard_config WHERE id = 'singleton'`).Scan(
		&meta.LastRefreshedAt,
		&meta.LastRefreshedBy,
		&capsJSON,
		&meta.MembershipStrategy,
		&meta.RefreshIntervalMs,
	)
	if err != nil {
		return meta, err
	}
	_ = json.Unmarshal(capsJSON, &meta.KnownCapabilities)
	if meta.LastRefreshedAt != nil {
		meta.DatasetVersion = meta.LastRefreshedAt.UnixMilli()
	}
	return meta, nil
}

// FlatRow is a row to insert into leaderboard_dataset_rows.
type FlatRow struct {
	Capability   string
	OrchURI      string
	GPUName      string
	GPUGb        float64
	Avail        float64
	TotalCap     float64
	PricePerUnit float64
	BestLatMs    *float64
	AvgLatMs     *float64
	SwapRatio    *float64
	AvgAvail     *float64
	Score        float64
}

const batchSize = 500

// WriteDataset replaces the full materialized dataset.
func (s *Store) WriteDataset(ctx context.Context, capabilities map[string][]FlatRow, refreshedBy string) (totalRows int, totalCaps int, err error) {
	capNames := make([]string, 0, len(capabilities))
	for cap := range capabilities {
		capNames = append(capNames, cap)
	}
	sort.Strings(capNames)

	var flat []FlatRow
	for _, cap := range capNames {
		for _, r := range capabilities[cap] {
			if r.OrchURI == "" {
				continue
			}
			r.Capability = cap
			flat = append(flat, r)
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM leaderboard_dataset_rows`); err != nil {
		return 0, 0, err
	}

	for i := 0; i < len(flat); i += batchSize {
		end := i + batchSize
		if end > len(flat) {
			end = len(flat)
		}
		for _, r := range flat[i:end] {
			_, err := tx.Exec(ctx, `
				INSERT INTO leaderboard_dataset_rows (
					capability, orch_uri, gpu_name, gpu_gb, avail, total_cap,
					price_per_unit, best_lat_ms, avg_lat_ms, swap_ratio, avg_avail, score
				) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
				r.Capability, r.OrchURI, r.GPUName, r.GPUGb, r.Avail, r.TotalCap,
				r.PricePerUnit, r.BestLatMs, r.AvgLatMs, r.SwapRatio, r.AvgAvail, r.Score,
			)
			if err != nil {
				return 0, 0, err
			}
		}
	}

	capsJSON, _ := json.Marshal(capNames)
	_, err = tx.Exec(ctx, `
		INSERT INTO leaderboard_config (id, last_refreshed_at, last_refreshed_by, known_capabilities)
		VALUES ('singleton', now(), $1, $2::jsonb)
		ON CONFLICT (id) DO UPDATE SET
			last_refreshed_at = EXCLUDED.last_refreshed_at,
			last_refreshed_by = EXCLUDED.last_refreshed_by,
			known_capabilities = EXCLUDED.known_capabilities`,
		refreshedBy, string(capsJSON),
	)
	if err != nil {
		return 0, 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return len(flat), len(capNames), nil
}

// QueryRows fetches dataset rows for one capability with SQL filters.
func (s *Store) QueryRows(ctx context.Context, capability string, filters QueryFilters, limit int) ([]FlatRow, error) {
	q := `
		SELECT orch_uri, gpu_name, gpu_gb, avail, total_cap, price_per_unit,
			best_lat_ms, avg_lat_ms, swap_ratio, avg_avail, score
		FROM leaderboard_dataset_rows WHERE capability = $1`
	args := []any{capability}
	n := 2

	if filters.GPURamGbMin != nil {
		q += fmt.Sprintf(" AND gpu_gb >= $%d", n)
		args = append(args, *filters.GPURamGbMin)
		n++
	}
	if filters.GPURamGbMax != nil {
		q += fmt.Sprintf(" AND gpu_gb <= $%d", n)
		args = append(args, *filters.GPURamGbMax)
		n++
	}
	if filters.PriceMax != nil {
		q += fmt.Sprintf(" AND price_per_unit <= $%d", n)
		args = append(args, *filters.PriceMax)
		n++
	}
	if filters.MaxAvgLatencyMs != nil {
		q += fmt.Sprintf(" AND avg_lat_ms IS NOT NULL AND avg_lat_ms <= $%d", n)
		args = append(args, *filters.MaxAvgLatencyMs)
		n++
	}
	if filters.MaxSwapRatio != nil {
		q += fmt.Sprintf(" AND swap_ratio IS NOT NULL AND swap_ratio <= $%d", n)
		args = append(args, *filters.MaxSwapRatio)
		n++
	}

	q += fmt.Sprintf(" LIMIT $%d", n)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FlatRow
	for rows.Next() {
		var r FlatRow
		r.Capability = capability
		if err := rows.Scan(
			&r.OrchURI, &r.GPUName, &r.GPUGb, &r.Avail, &r.TotalCap, &r.PricePerUnit,
			&r.BestLatMs, &r.AvgLatMs, &r.SwapRatio, &r.AvgAvail, &r.Score,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListCapabilities returns distinct capabilities in the dataset.
func (s *Store) ListCapabilities(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT capability FROM leaderboard_dataset_rows ORDER BY capability`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var caps []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		caps = append(caps, c)
	}
	if len(caps) > 0 {
		return caps, rows.Err()
	}
	meta, err := s.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	return meta.KnownCapabilities, nil
}

// WriteAudit persists a refresh audit record.
func (s *Store) WriteAudit(ctx context.Context, refreshedBy string, durationMs int64, auditJSON []byte, perSourceJSON []byte) error {
	var audit map[string]any
	_ = json.Unmarshal(auditJSON, &audit)
	membership, _ := audit["membershipSource"].(string)
	totalOrch, _ := audit["totalOrchestrators"].(float64)
	totalCaps, _ := audit["totalCapabilities"].(float64)
	conflicts, _ := json.Marshal(audit["conflicts"])
	dropped, _ := json.Marshal(audit["dropped"])
	warnings, _ := json.Marshal(audit["warnings"])

	_, err := s.pool.Exec(ctx, `
		INSERT INTO leaderboard_refresh_audit (
			refreshed_by, duration_ms, membership_source,
			total_orchestrators, total_capabilities,
			per_source, conflicts, dropped, warnings
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		refreshedBy, durationMs, membership,
		int(totalOrch), int(totalCaps),
		perSourceJSON, conflicts, dropped, warnings,
	)
	return err
}

// QueryFilters are SQL-level filter predicates.
type QueryFilters struct {
	GPURamGbMin     *float64
	GPURamGbMax     *float64
	PriceMax        *float64
	MaxAvgLatencyMs *float64
	MaxSwapRatio    *float64
}

// DatasetStats for health/freshness endpoints.
type DatasetStats struct {
	Populated       bool
	RefreshedAt     *time.Time
	RefreshedBy     string
	TotalRows       int
	CapabilityCount int
}

// GetStats returns dataset population stats.
func (s *Store) GetStats(ctx context.Context) (DatasetStats, error) {
	var stats DatasetStats
	meta, err := s.GetConfig(ctx)
	if err != nil {
		return stats, err
	}
	stats.RefreshedAt = meta.LastRefreshedAt
	stats.RefreshedBy = meta.LastRefreshedBy
	stats.CapabilityCount = len(meta.KnownCapabilities)

	err = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM leaderboard_dataset_rows`).Scan(&stats.TotalRows)
	if err != nil {
		return stats, err
	}
	stats.Populated = stats.TotalRows > 0 && meta.LastRefreshedAt != nil
	return stats, nil
}
