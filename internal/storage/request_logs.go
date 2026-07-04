package storage

import (
	"database/sql"
	"fmt"
	"time"
)

func InsertRequestLog(db *sql.DB, log *RequestLog) error {
	_, err := db.Exec(
		`INSERT INTO request_logs (request_id, provider_name, provider_type, requested_model, actual_model, stream, status, error_message, created_at, finished_at, time_to_first_token_ms, total_duration_ms, input_tokens, output_tokens, cached_tokens, cache_write_tokens, total_tokens, client_ip, gateway_api_key_name)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.RequestID, log.ProviderName, log.ProviderType, log.RequestedModel, log.ActualModel,
		boolToInt(log.Stream), log.Status, log.ErrorMessage, log.CreatedAt, log.FinishedAt,
		log.TimeToFirstTokenMs, log.TotalDurationMs, log.InputTokens, log.OutputTokens, log.CachedTokens, log.CacheWriteTokens, log.TotalTokens, log.ClientIP, log.GatewayAPIKeyName,
	)
	if err != nil {
		return fmt.Errorf("insert request log: %w", err)
	}
	return nil
}

func UpdateRequestLog(db *sql.DB, log *RequestLog) error {
	_, err := db.Exec(
		`UPDATE request_logs SET status=?, error_message=?, finished_at=?, time_to_first_token_ms=?, total_duration_ms=?, input_tokens=?, output_tokens=?, cached_tokens=?, cache_write_tokens=?, total_tokens=? WHERE request_id=?`,
		log.Status, log.ErrorMessage, log.FinishedAt, log.TimeToFirstTokenMs, log.TotalDurationMs,
		log.InputTokens, log.OutputTokens, log.CachedTokens, log.CacheWriteTokens, log.TotalTokens, log.RequestID,
	)
	if err != nil {
		return fmt.Errorf("update request log: %w", err)
	}
	return nil
}

type RequestLogFilter struct {
	Limit  int
	Offset int
}

func ListRequestLogs(db *sql.DB, filter RequestLogFilter) ([]RequestLog, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	rows, err := db.Query(
		`SELECT id, request_id, provider_name, provider_type, requested_model, actual_model, stream, status, error_message, created_at, finished_at, time_to_first_token_ms, total_duration_ms, input_tokens, output_tokens, cached_tokens, cache_write_tokens, total_tokens, client_ip, gateway_api_key_name
		 FROM request_logs ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		filter.Limit, filter.Offset,
	)
	if err != nil {
		return nil, fmt.Errorf("list request logs: %w", err)
	}
	defer rows.Close()

	var logs []RequestLog
	for rows.Next() {
		var l RequestLog
		if err := rows.Scan(&l.ID, &l.RequestID, &l.ProviderName, &l.ProviderType, &l.RequestedModel, &l.ActualModel,
			&l.Stream, &l.Status, &l.ErrorMessage, &l.CreatedAt, &l.FinishedAt,
			&l.TimeToFirstTokenMs, &l.TotalDurationMs, &l.InputTokens, &l.OutputTokens, &l.CachedTokens, &l.CacheWriteTokens, &l.TotalTokens, &l.ClientIP, &l.GatewayAPIKeyName); err != nil {
			return nil, fmt.Errorf("scan request log: %w", err)
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

// EarliestRequestLogTime returns the earliest created_at across all logs (UTC).
// Returns zero time when there are no logs.
func EarliestRequestLogTime(db *sql.DB) (time.Time, error) {
	var s sql.NullString
	if err := db.QueryRow(`SELECT MIN(created_at) FROM request_logs`).Scan(&s); err != nil {
		return time.Time{}, fmt.Errorf("earliest request log time: %w", err)
	}
	if !s.Valid || s.String == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s.String)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse earliest created_at: %w", err)
	}
	return t, nil
}

// --- Stats summary (range-aware) ---

// WindowStats groups per-window totals.
type WindowStats struct {
	Requests           int64 `json:"requests"`
	SuccessfulRequests int64 `json:"successful_requests"`
	FailedRequests     int64 `json:"failed_requests"`
	Tokens             int64 `json:"tokens"`
	InputTokens        int64 `json:"input_tokens"`
	OutputTokens       int64 `json:"output_tokens"`
	CachedTokens       int64 `json:"cached_tokens"`
	AvgDurationMs      float64 `json:"avg_duration_ms"`
	AvgTtftMs          float64 `json:"avg_ttft_ms"`
	Start              string  `json:"start,omitempty"`
	End                string  `json:"end,omitempty"`
}

// StatsSummary is the dashboard payload keyed by a time range.
type StatsSummary struct {
	Range             string           `json:"range"`
	BucketKind        string           `json:"bucket_kind"`
	Timezone          string           `json:"timezone"`
	HasPreviousWindow bool             `json:"has_previous_window"`
	ActiveDays        int64            `json:"active_days"`
	Current           WindowStats      `json:"current"`
	Previous          WindowStats      `json:"previous"`
	RequestsByProvider map[string]int64 `json:"requests_by_provider"`
	RequestsByModel    map[string]int64 `json:"requests_by_model"`
	TokensByProvider   map[string]int64 `json:"tokens_by_provider"`
	TokensByModel      map[string]int64 `json:"tokens_by_model"`
	Series             []DayCount       `json:"series"`
	TokenSeries        []DayCount       `json:"token_series"`
	ModelPerformance   []ModelPerf      `json:"model_performance"`
	ProviderPerformance []ModelPerf     `json:"provider_performance"`
}

type DayCount struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

type ModelPerf struct {
	Model           string  `json:"model"`
	TokensPerSecond float64 `json:"tokens_per_second"`
	AvgTtftMs       float64 `json:"avg_ttft_ms"`
	SampleCount     int64   `json:"sample_count"`
}

// StatsParams captures inputs needed to build a range-aware summary.
type StatsParams struct {
	Range      string
	Timezone   string
	Loc        *time.Location
	CurStart   time.Time
	CurEnd     time.Time
	HasPrev    bool
	PrevStart  time.Time
	PrevEnd    time.Time
	BucketKind string
	// SeriesStart / SeriesEnd bound the time-series chart independently from the
	// aggregate window. For range=all this can be a shorter lookback so the chart
	// stays readable while aggregates still cover everything.
	SeriesStart time.Time
	SeriesEnd   time.Time
	// Buckets is a pre-computed ordered list of local bucket boundaries covering
	// [SeriesStart, SeriesEnd]. Each bucket applies to [Start, End) and yields a Date label.
	Buckets []Bucket
}

// Bucket is a single time-series bucket derived from the current window.
type Bucket struct {
	Start time.Time
	End   time.Time
	Label string
}

// GetStatsSummary builds a StatsSummary for the given params.
func GetStatsSummary(db *sql.DB, p StatsParams) (*StatsSummary, error) {
	if p.Loc == nil {
		p.Loc = time.UTC
	}
	s := &StatsSummary{
		Range:             p.Range,
		BucketKind:        p.BucketKind,
		Timezone:          p.Timezone,
		HasPreviousWindow: p.HasPrev,
		RequestsByProvider: map[string]int64{},
		RequestsByModel:    map[string]int64{},
		TokensByProvider:   map[string]int64{},
		TokensByModel:      map[string]int64{},
		ModelPerformance:   []ModelPerf{},
		ProviderPerformance: []ModelPerf{},
	}

	curStartStr := p.CurStart.UTC().Format(time.RFC3339)
	curEndStr := p.CurEnd.UTC().Format(time.RFC3339)
	s.Current.Start = curStartStr
	s.Current.End = curEndStr

	// Current window aggregates
	if err := scanWindow(db, curStartStr, curEndStr, &s.Current); err != nil {
		return nil, fmt.Errorf("current window: %w", err)
	}

	// Previous window aggregates
	if p.HasPrev {
		prevStartStr := p.PrevStart.UTC().Format(time.RFC3339)
		prevEndStr := p.PrevEnd.UTC().Format(time.RFC3339)
		s.Previous.Start = prevStartStr
		s.Previous.End = prevEndStr
		if err := scanWindow(db, prevStartStr, prevEndStr, &s.Previous); err != nil {
			return nil, fmt.Errorf("previous window: %w", err)
		}
	}

	// Distribution: requests + tokens per provider within current window
	provRows, err := db.Query(`
		SELECT provider_name, COUNT(*), COALESCE(SUM(total_tokens), 0)
		FROM request_logs
		WHERE created_at >= ? AND created_at < ?
		GROUP BY provider_name
	`, curStartStr, curEndStr)
	if err != nil {
		return nil, fmt.Errorf("distribution provider: %w", err)
	}
	for provRows.Next() {
		var name string
		var count, tokens int64
		if err := provRows.Scan(&name, &count, &tokens); err != nil {
			provRows.Close()
			return nil, fmt.Errorf("scan distribution provider: %w", err)
		}
		s.RequestsByProvider[name] = count
		s.TokensByProvider[name] = tokens
	}
	provRows.Close()

	modelRows, err := db.Query(`
		SELECT requested_model, COUNT(*), COALESCE(SUM(total_tokens), 0)
		FROM request_logs
		WHERE created_at >= ? AND created_at < ?
		GROUP BY requested_model
	`, curStartStr, curEndStr)
	if err != nil {
		return nil, fmt.Errorf("distribution model: %w", err)
	}
	for modelRows.Next() {
		var name string
		var count, tokens int64
		if err := modelRows.Scan(&name, &count, &tokens); err != nil {
			modelRows.Close()
			return nil, fmt.Errorf("scan distribution model: %w", err)
		}
		s.RequestsByModel[name] = count
		s.TokensByModel[name] = tokens
	}
	modelRows.Close()

	// Performance TOP 5 (model + provider) within current window
	mp, err := scanPerf(db, "requested_model", curStartStr, curEndStr)
	if err != nil {
		return nil, fmt.Errorf("model performance: %w", err)
	}
	s.ModelPerformance = mp

	pp, err := scanPerf(db, "provider_name", curStartStr, curEndStr)
	if err != nil {
		return nil, fmt.Errorf("provider performance: %w", err)
	}
	s.ProviderPerformance = pp

	// Series: pull created_at + total_tokens rows, bucketize in Go using loc.
	seriesStart := p.SeriesStart
	seriesEnd := p.SeriesEnd
	if seriesStart.IsZero() {
		seriesStart = p.CurStart
	}
	if seriesEnd.IsZero() {
		seriesEnd = p.CurEnd
	}
	series, tokenSeries, err := scanSeries(db,
		seriesStart.UTC().Format(time.RFC3339),
		seriesEnd.UTC().Format(time.RFC3339),
		p.Buckets, p.Loc)
	if err != nil {
		return nil, fmt.Errorf("series: %w", err)
	}
	s.Series = series
	s.TokenSeries = tokenSeries

	// Active days (used for all-time daily average). Compute using loc: for each log,
	// convert created_at to loc and count distinct local dates. Data volume is small.
	activeDays, err := countActiveDays(db, curStartStr, curEndStr, p.Loc)
	if err != nil {
		return nil, fmt.Errorf("active days: %w", err)
	}
	s.ActiveDays = activeDays

	return s, nil
}

func scanWindow(db *sql.DB, start, end string, w *WindowStats) error {
	return db.QueryRow(`
		SELECT
			COALESCE(COUNT(*), 0),
			COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'error'   THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(total_tokens), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(cached_tokens), 0),
			COALESCE(AVG(total_duration_ms), 0),
			COALESCE(AVG(time_to_first_token_ms), 0)
		FROM request_logs
		WHERE created_at >= ? AND created_at < ?
	`, start, end).Scan(
		&w.Requests, &w.SuccessfulRequests, &w.FailedRequests,
		&w.Tokens, &w.InputTokens, &w.OutputTokens, &w.CachedTokens,
		&w.AvgDurationMs, &w.AvgTtftMs,
	)
}

func scanPerf(db *sql.DB, groupCol, start, end string) ([]ModelPerf, error) {
	q := fmt.Sprintf(`
		SELECT
			%s AS name,
			SUM(output_tokens) * 1000.0
				/ NULLIF(SUM(total_duration_ms - COALESCE(time_to_first_token_ms, 0)), 0) AS tps,
			AVG(time_to_first_token_ms) AS ttft,
			COUNT(*) AS n
		FROM request_logs
		WHERE status = 'success'
			AND output_tokens > 0
			AND total_duration_ms > 0
			AND (total_duration_ms - COALESCE(time_to_first_token_ms, 0)) > 0
			AND created_at >= ? AND created_at < ?
		GROUP BY %s
		HAVING tps IS NOT NULL
		ORDER BY tps DESC
		LIMIT 5
	`, groupCol, groupCol)
	rows, err := db.Query(q, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ModelPerf, 0, 5)
	for rows.Next() {
		var mp ModelPerf
		var ttft sql.NullFloat64
		if err := rows.Scan(&mp.Model, &mp.TokensPerSecond, &ttft, &mp.SampleCount); err != nil {
			return nil, err
		}
		if ttft.Valid {
			mp.AvgTtftMs = ttft.Float64
		}
		out = append(out, mp)
	}
	return out, rows.Err()
}

func scanSeries(db *sql.DB, start, end string, buckets []Bucket, loc *time.Location) ([]DayCount, []DayCount, error) {
	series := make([]DayCount, len(buckets))
	tokenSeries := make([]DayCount, len(buckets))
	for i, b := range buckets {
		series[i].Date = b.Label
		tokenSeries[i].Date = b.Label
	}
	if len(buckets) == 0 {
		return series, tokenSeries, nil
	}

	rows, err := db.Query(`
		SELECT created_at, total_tokens
		FROM request_logs
		WHERE created_at >= ? AND created_at < ?
	`, start, end)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var createdAt string
		var tokens int64
		if err := rows.Scan(&createdAt, &tokens); err != nil {
			return nil, nil, err
		}
		t, err := time.Parse(time.RFC3339, createdAt)
		if err != nil {
			continue
		}
		local := t.In(loc)
		idx := findBucket(buckets, local)
		if idx < 0 {
			continue
		}
		series[idx].Count++
		tokenSeries[idx].Count += tokens
	}
	return series, tokenSeries, rows.Err()
}

func findBucket(buckets []Bucket, t time.Time) int {
	// Buckets are ordered ascending and non-overlapping.
	// Linear scan is fine for small N (<=31 typically).
	for i, b := range buckets {
		if !t.Before(b.Start) && t.Before(b.End) {
			return i
		}
	}
	return -1
}

func countActiveDays(db *sql.DB, start, end string, loc *time.Location) (int64, error) {
	rows, err := db.Query(`SELECT created_at FROM request_logs WHERE created_at >= ? AND created_at < ?`, start, end)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	dates := make(map[string]struct{})
	for rows.Next() {
		var createdAt string
		if err := rows.Scan(&createdAt); err != nil {
			return 0, err
		}
		t, err := time.Parse(time.RFC3339, createdAt)
		if err != nil {
			continue
		}
		local := t.In(loc)
		key := local.Format("2006-01-02")
		dates[key] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return int64(len(dates)), nil
}
