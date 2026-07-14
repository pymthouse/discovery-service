package db

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
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
	LastRefreshedAt        *time.Time
	LastRefreshedBy        string
	KnownCapabilities      []string
	KnownCapabilityEntries []CapabilityEntry
	MembershipStrategy     string
	RefreshIntervalMs      int64
	DatasetVersion         int64
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
	if err := json.Unmarshal(capsJSON, &meta.KnownCapabilityEntries); err != nil || len(meta.KnownCapabilityEntries) == 0 {
		_ = json.Unmarshal(capsJSON, &meta.KnownCapabilities)
		for _, cap := range meta.KnownCapabilities {
			meta.KnownCapabilityEntries = append(meta.KnownCapabilityEntries, CapabilityEntry{
				ServiceType: "live-video-to-video",
				Capability:  cap,
			})
		}
	} else {
		meta.KnownCapabilities = capabilityNamesFromEntries(meta.KnownCapabilityEntries)
	}
	if meta.LastRefreshedAt != nil {
		meta.DatasetVersion = meta.LastRefreshedAt.UnixMilli()
	}
	return meta, nil
}

// CapabilityEntry summarizes one capability namespace in the dataset.
type CapabilityEntry struct {
	ServiceType string   `json:"serviceType"`
	Capability  string   `json:"capability"`
	OfferingIDs []string `json:"offeringIds,omitempty"`
}

// FlatRow is a row to insert into leaderboard_dataset_rows.
type FlatRow struct {
	ServiceType     string
	Capability      string
	EthAddress      string
	OrchURI         string
	OfferingID      string
	InteractionMode string
	WorkUnit        string
	PricePerUnitWei string
	GPUName         string
	GPUGb           float64
	Avail           float64
	TotalCap        float64
	PricePerUnit    float64
	BestLatMs       *float64
	AvgLatMs        *float64
	SwapRatio       *float64
	AvgAvail        *float64
	Score           float64
}

const batchSize = 500

func normalizeFlatRows(rows []FlatRow) []FlatRow {
	flat := make([]FlatRow, 0, len(rows))
	for _, r := range rows {
		if r.OrchURI == "" || r.Capability == "" {
			continue
		}
		if r.ServiceType == "" {
			r.ServiceType = "live-video-to-video"
		}
		flat = append(flat, r)
	}
	return flat
}

func buildCapabilityEntries(rows []FlatRow) []CapabilityEntry {
	type key struct {
		serviceType string
		capability  string
	}
	offerings := make(map[key]map[string]struct{})
	order := make([]key, 0)
	for _, r := range rows {
		k := key{serviceType: r.ServiceType, capability: r.Capability}
		if _, ok := offerings[k]; !ok {
			offerings[k] = make(map[string]struct{})
			order = append(order, k)
		}
		if r.OfferingID != "" {
			offerings[k][r.OfferingID] = struct{}{}
		}
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].serviceType != order[j].serviceType {
			return order[i].serviceType < order[j].serviceType
		}
		return order[i].capability < order[j].capability
	})
	entries := make([]CapabilityEntry, 0, len(order))
	for _, k := range order {
		entry := CapabilityEntry{
			ServiceType: k.serviceType,
			Capability:  k.capability,
		}
		for off := range offerings[k] {
			entry.OfferingIDs = append(entry.OfferingIDs, off)
		}
		sort.Strings(entry.OfferingIDs)
		entries = append(entries, entry)
	}
	return entries
}

func capabilityNamesFromEntries(entries []CapabilityEntry) []string {
	names := make([]string, 0, len(entries))
	seen := make(map[string]struct{})
	for _, e := range entries {
		if _, ok := seen[e.Capability]; ok {
			continue
		}
		seen[e.Capability] = struct{}{}
		names = append(names, e.Capability)
	}
	sort.Strings(names)
	return names
}

func (s *Store) insertFlatRows(ctx context.Context, tx pgx.Tx, flat []FlatRow) error {
	for i := 0; i < len(flat); i += batchSize {
		end := i + batchSize
		if end > len(flat) {
			end = len(flat)
		}
		for _, r := range flat[i:end] {
			_, err := tx.Exec(ctx, `
				INSERT INTO leaderboard_dataset_rows (
					service_type, capability, orch_uri, eth_address, offering_id,
					interaction_mode, work_unit, price_per_unit_wei,
					gpu_name, gpu_gb, avail, total_cap,
					price_per_unit, best_lat_ms, avg_lat_ms, swap_ratio, avg_avail, score
				) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
				r.ServiceType, r.Capability, r.OrchURI, r.EthAddress, r.OfferingID,
				r.InteractionMode, r.WorkUnit, r.PricePerUnitWei,
				r.GPUName, r.GPUGb, r.Avail, r.TotalCap,
				r.PricePerUnit, r.BestLatMs, r.AvgLatMs, r.SwapRatio, r.AvgAvail, r.Score,
			)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) updateDatasetConfig(ctx context.Context, tx pgx.Tx, refreshedBy string, entries []CapabilityEntry) error {
	capsJSON, _ := json.Marshal(entries)
	_, err := tx.Exec(ctx, `
		INSERT INTO leaderboard_config (id, last_refreshed_at, last_refreshed_by, known_capabilities)
		VALUES ('singleton', now(), $1, $2::jsonb)
		ON CONFLICT (id) DO UPDATE SET
			last_refreshed_at = EXCLUDED.last_refreshed_at,
			last_refreshed_by = EXCLUDED.last_refreshed_by,
			known_capabilities = EXCLUDED.known_capabilities`,
		refreshedBy, string(capsJSON),
	)
	return err
}

// WriteDataset replaces the full materialized dataset.
func (s *Store) WriteDataset(ctx context.Context, rows []FlatRow, refreshedBy string) (totalRows int, totalCaps int, err error) {
	flat := normalizeFlatRows(rows)
	entries := buildCapabilityEntries(flat)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && err != pgx.ErrTxClosed {
			return
		}
	}()

	if _, err := tx.Exec(ctx, `DELETE FROM leaderboard_dataset_rows`); err != nil {
		return 0, 0, err
	}
	if err := s.insertFlatRows(ctx, tx, flat); err != nil {
		return 0, 0, err
	}
	if err := s.updateDatasetConfig(ctx, tx, refreshedBy, entries); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return len(flat), len(entries), nil
}

// QueryRows fetches dataset rows for one capability with SQL filters.
func (s *Store) QueryRows(ctx context.Context, capability string, serviceTypes []string, filters QueryFilters, limit int) ([]FlatRow, error) {
	q := `
		SELECT service_type, capability, orch_uri, eth_address, offering_id,
			interaction_mode, work_unit, price_per_unit_wei,
			gpu_name, gpu_gb, avail, total_cap, price_per_unit,
			best_lat_ms, avg_lat_ms, swap_ratio, avg_avail, score
		FROM leaderboard_dataset_rows WHERE capability = $1`
	args := []any{capability}
	n := 2

	if len(serviceTypes) > 0 {
		q += fmt.Sprintf(" AND service_type = ANY($%d)", n)
		args = append(args, serviceTypes)
		n++
	}

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
		if err := rows.Scan(
			&r.ServiceType, &r.Capability, &r.OrchURI, &r.EthAddress, &r.OfferingID,
			&r.InteractionMode, &r.WorkUnit, &r.PricePerUnitWei,
			&r.GPUName, &r.GPUGb, &r.Avail, &r.TotalCap, &r.PricePerUnit,
			&r.BestLatMs, &r.AvgLatMs, &r.SwapRatio, &r.AvgAvail, &r.Score,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListCapabilities returns distinct capability names in the dataset.
func (s *Store) ListCapabilities(ctx context.Context, serviceTypes []string) ([]string, error) {
	entries, err := s.ListCapabilityEntries(ctx, serviceTypes)
	if err != nil {
		return nil, err
	}
	return capabilityNamesFromEntries(entries), nil
}

// ListCapabilityEntries returns capability summaries optionally filtered by service type.
func (s *Store) ListCapabilityEntries(ctx context.Context, serviceTypes []string) ([]CapabilityEntry, error) {
	q := `
		SELECT service_type, capability, offering_id
		FROM leaderboard_dataset_rows`
	args := []any{}
	if len(serviceTypes) > 0 {
		q += ` WHERE service_type = ANY($1)`
		args = append(args, serviceTypes)
	}
	q += ` ORDER BY service_type, capability, offering_id`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	flat := make([]FlatRow, 0)
	for rows.Next() {
		var r FlatRow
		if err := rows.Scan(&r.ServiceType, &r.Capability, &r.OfferingID); err != nil {
			return nil, err
		}
		flat = append(flat, r)
	}
	if len(flat) > 0 {
		return buildCapabilityEntries(flat), rows.Err()
	}

	meta, err := s.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	if len(serviceTypes) == 0 {
		return meta.KnownCapabilityEntries, nil
	}
	filtered := make([]CapabilityEntry, 0)
	for _, e := range meta.KnownCapabilityEntries {
		for _, st := range serviceTypes {
			if e.ServiceType == st {
				filtered = append(filtered, e)
				break
			}
		}
	}
	return filtered, nil
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
