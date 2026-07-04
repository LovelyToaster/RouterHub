package admin

import (
	"time"

	"github.com/lovelytoaster94/routerhub/internal/storage"
)// RangeKey represents a supported stats time range.
type RangeKey string

const (
	RangeAll   RangeKey = "all"
	RangeMonth RangeKey = "month"
	RangeWeek  RangeKey = "week"
	RangeDay   RangeKey = "day"
)

// ParseRange normalizes a query parameter to a supported RangeKey.
// Unknown or empty values default to RangeAll.
func ParseRange(s string) RangeKey {
	switch RangeKey(s) {
	case RangeAll, RangeMonth, RangeWeek, RangeDay:
		return RangeKey(s)
	}
	return RangeAll
}

// LoadUserLocation returns the effective *time.Location for a user.
// An empty timezone or unloadable name falls back to time.Local.
func LoadUserLocation(u *storage.AdminUser) *time.Location {
	if u == nil || u.Timezone == "" {
		return time.Local
	}
	loc, err := time.LoadLocation(u.Timezone)
	if err != nil {
		return time.Local
	}
	return loc
}

// BucketKind describes the time-series bucketization used for a range.
type BucketKind string

const (
	BucketHour  BucketKind = "hour"
	BucketDay   BucketKind = "day"
	BucketWeek  BucketKind = "week"
	BucketMonth BucketKind = "month"
)

// Window describes the current and previous comparison time windows for a range.
//
// - Cur[Start,End) is the current window (start-of-period to now).
// - Prev[Start,End) is the aligned previous window (same offset within the previous full period).
// - HasPrev is false for RangeAll or when a previous window cannot be computed.
// - Bucket controls the granularity of the time-series buckets.
// - SeriesStart/SeriesEnd bound the time-series chart. For RangeAll this is
//   a shorter lookback than [CurStart, CurEnd] so the chart stays readable.
type Window struct {
	Range       RangeKey
	Loc         *time.Location
	CurStart    time.Time
	CurEnd      time.Time
	PrevStart   time.Time
	PrevEnd     time.Time
	HasPrev     bool
	Bucket      BucketKind
	SeriesStart time.Time
	SeriesEnd   time.Time
}

// ComputeWindow builds a Window for the given range using the user's location.
// `now` is the reference "current" time (usually time.Now()).
func ComputeWindow(now time.Time, loc *time.Location, r RangeKey) Window {
	if loc == nil {
		loc = time.Local
	}
	localNow := now.In(loc)

	switch r {
	case RangeDay:
		curStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
		curEnd := localNow
		delta := curEnd.Sub(curStart)
		prevStart := curStart.AddDate(0, 0, -1)
		prevEnd := prevStart.Add(delta)
		seriesEnd := curStart.AddDate(0, 0, 1) // fill up to tomorrow 00:00
		return Window{
			Range:       RangeDay,
			Loc:         loc,
			CurStart:    curStart,
			CurEnd:      curEnd,
			PrevStart:   prevStart,
			PrevEnd:     prevEnd,
			HasPrev:     true,
			Bucket:      BucketHour,
			SeriesStart: curStart,
			SeriesEnd:   seriesEnd,
		}
	case RangeWeek:
		// Week starts on Monday.
		weekday := int(localNow.Weekday())
		if weekday == 0 {
			weekday = 7 // Sunday -> 7 so we subtract 6 days to reach Monday
		}
		daysSinceMonday := weekday - 1
		curStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc).
			AddDate(0, 0, -daysSinceMonday)
		curEnd := localNow
		delta := curEnd.Sub(curStart)
		prevStart := curStart.AddDate(0, 0, -7)
		prevEnd := prevStart.Add(delta)
		seriesEnd := curStart.AddDate(0, 0, 7) // fill up to next Monday 00:00
		return Window{
			Range:       RangeWeek,
			Loc:         loc,
			CurStart:    curStart,
			CurEnd:      curEnd,
			PrevStart:   prevStart,
			PrevEnd:     prevEnd,
			HasPrev:     true,
			Bucket:      BucketDay,
			SeriesStart: curStart,
			SeriesEnd:   seriesEnd,
		}
	case RangeMonth:
		curStart := time.Date(localNow.Year(), localNow.Month(), 1, 0, 0, 0, 0, loc)
		curEnd := localNow
		delta := curEnd.Sub(curStart)
		prevStart := curStart.AddDate(0, -1, 0)
		// Cap prevEnd at the end of the previous month so we never spill into cur.
		prevMonthEnd := curStart // exclusive upper bound of previous month
		prevEnd := prevStart.Add(delta)
		if prevEnd.After(prevMonthEnd) {
			prevEnd = prevMonthEnd
		}
		seriesEnd := curStart.AddDate(0, 1, 0) // fill up to next month 1st 00:00
		return Window{
			Range:       RangeMonth,
			Loc:         loc,
			CurStart:    curStart,
			CurEnd:      curEnd,
			PrevStart:   prevStart,
			PrevEnd:     prevEnd,
			HasPrev:     true,
			Bucket:      BucketDay,
			SeriesStart: curStart,
			SeriesEnd:   seriesEnd,
		}
	default: // RangeAll
		// Aggregation covers everything up to now.
		curStart := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
		curEnd := localNow
		// Series bounds are set later by AdjustAllWindowForSeries once we know
		// the earliest log timestamp.
		return Window{
			Range:       RangeAll,
			Loc:         loc,
			CurStart:    curStart,
			CurEnd:      curEnd,
			HasPrev:     false,
			Bucket:      BucketDay,
			SeriesStart: time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -29),
			SeriesEnd:   time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1),
		}
	}
}

// BuildBuckets returns an ordered list of storage.Bucket covering
// [w.SeriesStart, w.SeriesEnd] according to the window's bucket kind.
// Bucket boundaries are aligned to local time.
func BuildBuckets(w Window) []storage.Bucket {
	loc := w.Loc
	if loc == nil {
		loc = time.UTC
	}
	start := w.SeriesStart
	end := w.SeriesEnd
	if start.IsZero() {
		start = w.CurStart
	}
	if end.IsZero() {
		end = w.CurEnd
	}
	switch w.Bucket {
	case BucketHour:
		return buildHourly(start, end, loc)
	case BucketDay:
		return buildDaily(start, end, loc)
	case BucketWeek:
		return buildWeekly(start, end, loc)
	case BucketMonth:
		return buildMonthly(start, end, loc)
	}
	return buildDaily(start, end, loc)
}

func buildHourly(start, end time.Time, loc *time.Location) []storage.Bucket {
	start = start.In(loc)
	end = end.In(loc)
	first := time.Date(start.Year(), start.Month(), start.Day(), start.Hour(), 0, 0, 0, loc)
	var out []storage.Bucket
	for t := first; t.Before(end); t = t.Add(time.Hour) {
		out = append(out, storage.Bucket{
			Start: t,
			End:   t.Add(time.Hour),
			Label: t.Format("2006-01-02 15:04"),
		})
	}
	return out
}

func buildDaily(start, end time.Time, loc *time.Location) []storage.Bucket {
	start = start.In(loc)
	end = end.In(loc)
	first := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
	var out []storage.Bucket
	for t := first; t.Before(end); t = t.AddDate(0, 0, 1) {
		out = append(out, storage.Bucket{
			Start: t,
			End:   t.AddDate(0, 0, 1),
			Label: t.Format("2006-01-02"),
		})
	}
	return out
}

func buildWeekly(start, end time.Time, loc *time.Location) []storage.Bucket {
	start = start.In(loc)
	end = end.In(loc)
	// Align to Monday.
	weekday := int(start.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	daysSinceMonday := weekday - 1
	first := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -daysSinceMonday)
	var out []storage.Bucket
	for t := first; t.Before(end); t = t.AddDate(0, 0, 7) {
		out = append(out, storage.Bucket{
			Start: t,
			End:   t.AddDate(0, 0, 7),
			Label: t.Format("2006-01-02"),
		})
	}
	return out
}

func buildMonthly(start, end time.Time, loc *time.Location) []storage.Bucket {
	start = start.In(loc)
	end = end.In(loc)
	first := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, loc)
	var out []storage.Bucket
	for t := first; t.Before(end); t = t.AddDate(0, 1, 0) {
		out = append(out, storage.Bucket{
			Start: t,
			End:   t.AddDate(0, 1, 0),
			Label: t.Format("2006-01"),
		})
	}
	return out
}

// AdjustAllWindowForSeries adapts the series bounds and bucket kind for RangeAll,
// given the earliest log time and current time. Strategy:
//   spanDays <= 90  -> day bucket
//   spanDays <= 730 -> week bucket
//   otherwise       -> month bucket
// The series always spans from the first log's local start-of-day to the local
// start of the next natural boundary after now, so the chart is fully filled.
func AdjustAllWindowForSeries(w Window, earliest time.Time, now time.Time) Window {
	if w.Range != RangeAll {
		return w
	}
	loc := w.Loc
	if loc == nil {
		loc = time.UTC
	}
	if earliest.IsZero() || earliest.After(now) {
		return w // no logs yet, keep the default 30-day fallback
	}

	first := earliest.In(loc)
	localNow := now.In(loc)
	spanDays := int(localNow.Sub(first).Hours()/24) + 1

	switch {
	case spanDays <= 90:
		w.Bucket = BucketDay
		w.SeriesStart = time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, loc)
		w.SeriesEnd = time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
	case spanDays <= 730:
		w.Bucket = BucketWeek
		// Snap start to Monday of the first log's week.
		weekday := int(first.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		w.SeriesStart = time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, loc).
			AddDate(0, 0, -(weekday - 1))
		// End at the start of next Monday after now.
		nowWeekday := int(localNow.Weekday())
		if nowWeekday == 0 {
			nowWeekday = 7
		}
		w.SeriesEnd = time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc).
			AddDate(0, 0, 7-(nowWeekday-1))
	default:
		w.Bucket = BucketMonth
		w.SeriesStart = time.Date(first.Year(), first.Month(), 1, 0, 0, 0, 0, loc)
		w.SeriesEnd = time.Date(localNow.Year(), localNow.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, 1, 0)
	}
	return w
}
