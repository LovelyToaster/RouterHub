package storage

import (
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

func InsertRequestLog(db *sql.DB, log *RequestLog) error {
	_, err := db.Exec(
		`INSERT INTO request_logs (request_id, provider_name, provider_type, inbound_protocol, requested_model, actual_model, stream, status, error_message, created_at, finished_at, time_to_first_token_ms, total_duration_ms, input_tokens, output_tokens, cached_tokens, cache_write_tokens, total_tokens, client_ip, gateway_api_key_name, http_status, request_body, response_body)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.RequestID, log.ProviderName, log.ProviderType, log.InboundProtocol, log.RequestedModel, log.ActualModel,
		boolToInt(log.Stream), log.Status, log.ErrorMessage, log.CreatedAt, log.FinishedAt,
		log.TimeToFirstTokenMs, log.TotalDurationMs, log.InputTokens, log.OutputTokens, log.CachedTokens, log.CacheWriteTokens, log.TotalTokens, log.ClientIP, log.GatewayAPIKeyName,
		log.HTTPStatus, log.RequestBody, log.ResponseBody,
	)
	if err != nil {
		return fmt.Errorf("insert request log: %w", err)
	}
	return nil
}

// UpdateRequestLog overwrites all mutable fields of an existing request log
// identified by RequestID. Used by the two-phase logging pipeline to finalize
// a "pending" row with success/error status plus timing and usage stats.
func UpdateRequestLog(db *sql.DB, log *RequestLog) error {
	_, err := db.Exec(
		`UPDATE request_logs SET actual_model=?, status=?, error_message=?, finished_at=?, time_to_first_token_ms=?, total_duration_ms=?, input_tokens=?, output_tokens=?, cached_tokens=?, cache_write_tokens=?, total_tokens=?, http_status=?, request_body=?, response_body=? WHERE request_id=?`,
		log.ActualModel, log.Status, log.ErrorMessage, log.FinishedAt, log.TimeToFirstTokenMs, log.TotalDurationMs,
		log.InputTokens, log.OutputTokens, log.CachedTokens, log.CacheWriteTokens, log.TotalTokens,
		log.HTTPStatus, log.RequestBody, log.ResponseBody, log.RequestID,
	)
	if err != nil {
		return fmt.Errorf("update request log: %w", err)
	}
	return nil
}

// MarkPendingLogsAsError transitions any leftover "pending" rows to "error".
// Called during startup so that logs abandoned by an interrupted process
// (crash, forced shutdown) do not stay stuck on "processing" forever.
func MarkPendingLogsAsError(db *sql.DB, message string) error {
	now := Now()
	_, err := db.Exec(
		`UPDATE request_logs SET status='error', error_message=?, finished_at=COALESCE(finished_at, ?) WHERE status='pending'`,
		message, now,
	)
	if err != nil {
		return fmt.Errorf("mark pending request logs as error: %w", err)
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
		`SELECT id, request_id, provider_name, provider_type, inbound_protocol, requested_model, actual_model, stream, status, error_message, created_at, finished_at, time_to_first_token_ms, total_duration_ms, input_tokens, output_tokens, cached_tokens, cache_write_tokens, total_tokens, client_ip, gateway_api_key_name, http_status, request_body, response_body
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
		if err := rows.Scan(&l.ID, &l.RequestID, &l.ProviderName, &l.ProviderType, &l.InboundProtocol, &l.RequestedModel, &l.ActualModel,
			&l.Stream, &l.Status, &l.ErrorMessage, &l.CreatedAt, &l.FinishedAt,
			&l.TimeToFirstTokenMs, &l.TotalDurationMs, &l.InputTokens, &l.OutputTokens, &l.CachedTokens, &l.CacheWriteTokens, &l.TotalTokens, &l.ClientIP, &l.GatewayAPIKeyName,
			&l.HTTPStatus, &l.RequestBody, &l.ResponseBody); err != nil {
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
		SELECT dimension, SUM(request_count), COALESCE(SUM(total_tokens), 0)
		FROM stats_counters
		WHERE dimension LIKE 'provider:%'
		  AND bucket >= ? AND bucket < ?
		GROUP BY dimension
	`, formatBucket(curStartStr), formatBucket(curEndStr))
	if err != nil {
		return nil, fmt.Errorf("distribution provider: %w", err)
	}
	for provRows.Next() {
		var dim string
		var count, tokens int64
		if err := provRows.Scan(&dim, &count, &tokens); err != nil {
			provRows.Close()
			return nil, fmt.Errorf("scan distribution provider: %w", err)
		}
		name := strings.TrimPrefix(dim, "provider:")
		s.RequestsByProvider[name] = count
		s.TokensByProvider[name] = tokens
	}
	provRows.Close()

	modelRows, err := db.Query(`
		SELECT dimension, SUM(request_count), COALESCE(SUM(total_tokens), 0)
		FROM stats_counters
		WHERE dimension LIKE 'model:%'
		  AND bucket >= ? AND bucket < ?
		GROUP BY dimension
	`, formatBucket(curStartStr), formatBucket(curEndStr))
	if err != nil {
		return nil, fmt.Errorf("distribution model: %w", err)
	}
	for modelRows.Next() {
		var dim string
		var count, tokens int64
		if err := modelRows.Scan(&dim, &count, &tokens); err != nil {
			modelRows.Close()
			return nil, fmt.Errorf("scan distribution model: %w", err)
		}
		name := strings.TrimPrefix(dim, "model:")
		s.RequestsByModel[name] = count
		s.TokensByModel[name] = tokens
	}
	modelRows.Close()

	// Performance TOP 5 (model + provider) within current window
	mp, err := scanPerf(db, "actual_model", curStartStr, curEndStr)
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

func formatBucket(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		if len(rfc3339) >= 13 {
			return rfc3339[:13]
		}
		return rfc3339
	}
	return t.UTC().Truncate(time.Hour).Format("2006-01-02T15")
}

func scanWindow(db *sql.DB, start, end string, w *WindowStats) error {
	startBucket := formatBucket(start)
	endBucket := formatBucket(end)
	return db.QueryRow(`
		SELECT
			COALESCE(SUM(request_count), 0),
			COALESCE(SUM(success_count), 0),
			COALESCE(SUM(error_count), 0),
			COALESCE(SUM(total_tokens), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(cached_tokens), 0),
			CASE WHEN SUM(request_count) > 0
			     THEN CAST(SUM(duration_sum_ms) AS REAL) / SUM(request_count)
			     ELSE 0 END,
			CASE WHEN SUM(request_count) > 0
			     THEN CAST(SUM(ttft_sum_ms) AS REAL) / SUM(request_count)
			     ELSE 0 END
		FROM stats_counters
		WHERE dimension = 'global'
		  AND bucket >= ? AND bucket < ?
	`, startBucket, endBucket).Scan(
		&w.Requests, &w.SuccessfulRequests, &w.FailedRequests,
		&w.Tokens, &w.InputTokens, &w.OutputTokens, &w.CachedTokens,
		&w.AvgDurationMs, &w.AvgTtftMs,
	)
}

func scanPerf(db *sql.DB, groupCol, start, end string) ([]ModelPerf, error) {
	var dimPrefix string
	if groupCol == "actual_model" {
		dimPrefix = "perf_model:"
	} else {
		dimPrefix = "perf_provider:"
	}

	type raw struct {
		dim          string
		outputTokens int64
		procMs       int64
		ttftSum      int64
		n            int64
	}

	rows, err := db.Query(`
		SELECT dimension,
		       SUM(perf_output_tokens),
		       SUM(perf_proc_ms),
		       SUM(perf_ttft_sum_ms),
		       SUM(perf_n)
		FROM stats_counters
		WHERE dimension LIKE ?
		  AND bucket >= ? AND bucket < ?
		  AND perf_n > 0
		GROUP BY dimension
	`, dimPrefix+"%", formatBucket(start), formatBucket(end))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var raws []raw
	for rows.Next() {
		var r raw
		if err := rows.Scan(&r.dim, &r.outputTokens, &r.procMs, &r.ttftSum, &r.n); err != nil {
			return nil, err
		}
		raws = append(raws, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]ModelPerf, 0, len(raws))
	for _, r := range raws {
		if r.procMs <= 0 {
			continue
		}
		tps := float64(r.outputTokens) * 1000.0 / float64(r.procMs)
		avgTtft := float64(0)
		if r.n > 0 {
			avgTtft = float64(r.ttftSum) / float64(r.n)
		}
		name := strings.TrimPrefix(r.dim, dimPrefix)
		out = append(out, ModelPerf{Model: name, TokensPerSecond: tps, AvgTtftMs: avgTtft, SampleCount: r.n})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].TokensPerSecond > out[j].TokensPerSecond })
	if len(out) > 5 {
		out = out[:5]
	}
	return out, nil
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
		SELECT bucket, request_count, total_tokens
		FROM stats_series
		WHERE bucket >= ? AND bucket < ?
		ORDER BY bucket
	`, formatBucket(start), formatBucket(end))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var bucket string
		var count, tokens int64
		if err := rows.Scan(&bucket, &count, &tokens); err != nil {
			return nil, nil, err
		}
		t, err := time.Parse("2006-01-02T15", bucket)
		if err != nil {
			continue
		}
		local := t.In(loc)
		idx := findBucket(buckets, local)
		if idx < 0 {
			continue
		}
		series[idx].Count += count
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
	rows, err := db.Query(`
		SELECT bucket FROM stats_series
		WHERE bucket >= ? AND bucket < ? AND request_count > 0
	`, formatBucket(start), formatBucket(end))
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	dates := make(map[string]struct{})
	for rows.Next() {
		var bucket string
		if err := rows.Scan(&bucket); err != nil {
			return 0, err
		}
		t, err := time.Parse("2006-01-02T15", bucket)
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

// DeleteOldRequestLogs 删除 created_at 早于 cutoff 的请求日志，返回删除行数。
func DeleteOldRequestLogs(db *sql.DB, days int) (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	res, err := db.Exec(`DELETE FROM request_logs WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete old request logs: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}

// StartRetentionWorker 周期性清理超龄请求日志。interval 建议 time.Hour。
func StartRetentionWorker(db *sql.DB, interval time.Duration) {
	// 启动立即执行一次
	runRetention(db)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		runRetention(db)
	}
}

func runRetention(db *sql.DB) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("retention worker panic: %v\n", r)
		}
	}()
	v := GetAppSettingString(db, "log.request_log_retention_days", "0")
	days, err := strconv.Atoi(v)
	if err != nil || days <= 0 {
		return
	}
	if n, err := DeleteOldRequestLogs(db, days); err != nil {
		fmt.Printf("retention cleanup error: %v\n", err)
	} else if n > 0 {
		fmt.Printf("retention cleanup removed %d request logs older than %d days\n", n, days)
	}
}

// DeleteOldStats deletes stats buckets older than the cutoff (in hours),
// determined by days. Returns total rows deleted from both stats tables.
func DeleteOldStats(db *sql.DB, days int) (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Truncate(time.Hour).Format("2006-01-02T15")
	res1, err := db.Exec(`DELETE FROM stats_counters WHERE bucket < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete old stats counters: %w", err)
	}
	n1, _ := res1.RowsAffected()
	res2, err := db.Exec(`DELETE FROM stats_series WHERE bucket < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete old stats series: %w", err)
	}
	n2, _ := res2.RowsAffected()
	return n1 + n2, nil
}

// StartStatsRetentionWorker periodically cleans up stats data older than the
// configured stats.retention_days setting. interval is typically time.Hour.
func StartStatsRetentionWorker(db *sql.DB, interval time.Duration) {
	runStatsRetention(db)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		runStatsRetention(db)
	}
}

func runStatsRetention(db *sql.DB) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("stats retention worker panic: %v\n", r)
		}
	}()
	v := GetAppSettingString(db, "stats.retention_days", "0")
	days, err := strconv.Atoi(v)
	if err != nil || days <= 0 {
		return
	}
	if n, err := DeleteOldStats(db, days); err != nil {
		fmt.Printf("stats retention cleanup error: %v\n", err)
	} else if n > 0 {
		fmt.Printf("stats retention cleanup removed %d bucket rows older than %d days\n", n, days)
	}
}
