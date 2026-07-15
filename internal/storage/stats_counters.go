package storage

import (
	"database/sql"
	"fmt"
	"time"
)

func UpsertStatsCounters(db *sql.DB, log *RequestLog) error {
	if log.FinishedAt == nil {
		return nil
	}
	finished, err := time.Parse(time.RFC3339, *log.FinishedAt)
	if err != nil {
		return fmt.Errorf("parse finished_at: %w", err)
	}
	bucket := finished.UTC().Truncate(time.Hour).Format("2006-01-02T15")

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	dims := []string{"global", "provider:" + log.ProviderName, "model:" + log.ActualModel}
	for _, dim := range dims {
		if err := upsertCounter(tx, dim, bucket, log, false, 0); err != nil {
			return fmt.Errorf("upsert counter %s: %w", dim, err)
		}
	}

	if log.Status == "success" && log.OutputTokens > 0 && log.TotalDurationMs != nil && *log.TotalDurationMs > 0 {
		ttft := int64(0)
		if log.TimeToFirstTokenMs != nil {
			ttft = *log.TimeToFirstTokenMs
		}
		proc := *log.TotalDurationMs - ttft
		if proc > 0 {
			for _, dim := range []string{"perf_model:" + log.ActualModel, "perf_provider:" + log.ProviderName} {
				if err := upsertCounter(tx, dim, bucket, log, true, proc); err != nil {
					return fmt.Errorf("upsert perf counter %s: %w", dim, err)
				}
			}
		}
	}

	if _, err := tx.Exec(
		`INSERT INTO stats_series (bucket, request_count, total_tokens) VALUES (?, 1, ?)
		 ON CONFLICT(bucket) DO UPDATE SET request_count = request_count + 1, total_tokens = total_tokens + excluded.total_tokens`,
		bucket, log.TotalTokens,
	); err != nil {
		return fmt.Errorf("upsert stats_series: %w", err)
	}

	return tx.Commit()
}

func upsertCounter(tx *sql.Tx, dim, bucket string, log *RequestLog, isPerf bool, procMs int64) error {
	var successVal, errorVal int64
	if log.Status == "success" {
		successVal = 1
	} else if log.Status == "error" {
		errorVal = 1
	}

	var durSum, ttftSum int64
	if log.TotalDurationMs != nil {
		durSum = *log.TotalDurationMs
	}
	if log.TimeToFirstTokenMs != nil {
		ttftSum = *log.TimeToFirstTokenMs
	}

	var perfOut, perfProc, perfTtft, perfN int64
	if isPerf {
		perfOut = log.OutputTokens
		perfProc = procMs
		if log.TimeToFirstTokenMs != nil {
			perfTtft = *log.TimeToFirstTokenMs
		}
		perfN = 1
	}

	_, err := tx.Exec(`
		INSERT INTO stats_counters (dimension, bucket,
			request_count, success_count, error_count,
			input_tokens, output_tokens, cached_tokens, cache_write_tokens, total_tokens,
			duration_sum_ms, ttft_sum_ms,
			perf_output_tokens, perf_proc_ms, perf_ttft_sum_ms, perf_n)
		VALUES (?, ?,
			?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?,
			?, ?, ?, ?)
		ON CONFLICT(dimension, bucket) DO UPDATE SET
			request_count        = request_count + excluded.request_count,
			success_count        = success_count + excluded.success_count,
			error_count          = error_count + excluded.error_count,
			input_tokens         = input_tokens + excluded.input_tokens,
			output_tokens        = output_tokens + excluded.output_tokens,
			cached_tokens        = cached_tokens + excluded.cached_tokens,
			cache_write_tokens   = cache_write_tokens + excluded.cache_write_tokens,
			total_tokens         = total_tokens + excluded.total_tokens,
			duration_sum_ms      = duration_sum_ms + excluded.duration_sum_ms,
			ttft_sum_ms          = ttft_sum_ms + excluded.ttft_sum_ms,
			perf_output_tokens   = perf_output_tokens + excluded.perf_output_tokens,
			perf_proc_ms         = perf_proc_ms + excluded.perf_proc_ms,
			perf_ttft_sum_ms     = perf_ttft_sum_ms + excluded.perf_ttft_sum_ms,
			perf_n               = perf_n + excluded.perf_n`,
		dim, bucket,
		1, successVal, errorVal,
		log.InputTokens, log.OutputTokens, log.CachedTokens, log.CacheWriteTokens, log.TotalTokens,
		durSum, ttftSum,
		perfOut, perfProc, perfTtft, perfN,
	)
	if err != nil {
		return fmt.Errorf("upsert counter row: %w", err)
	}
	return nil
}
