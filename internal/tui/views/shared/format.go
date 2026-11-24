package shared

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"postgres-monitor/internal/monitor"
)

// Relative renders a coarse age for a timestamp.
func Relative(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh ago", int(d.Hours()))
}

// Duration renders a compact duration.
func Duration(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	return d.Round(time.Second).String()
}

// IntervalShort renders a PostgreSQL interval such as 00:00:05 into a short form.
func IntervalShort(s string) string {
	parts := strings.Split(s, ":")
	if len(parts) == 3 {
		hours, e1 := strconv.Atoi(parts[0])
		minutes, e2 := strconv.Atoi(parts[1])
		seconds, e3 := strconv.ParseFloat(parts[2], 64)
		if e1 == nil && e2 == nil && e3 == nil {
			if hours > 0 {
				return fmt.Sprintf("%dh", hours)
			}
			if minutes > 0 {
				return fmt.Sprintf("%dm", minutes)
			}
			if seconds < 1 {
				return fmt.Sprintf("%.0fms", seconds*1000)
			}
			return fmt.Sprintf("%.1fs", seconds)
		}
	}
	return Truncate(s, 7)
}

// Rate renders a per-second byte rate.
func Rate(n float64) string {
	units := []string{"B/s", "KiB/s", "MiB/s", "GiB/s"}
	v := n
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

// BytesHuman renders a signed byte count.
func BytesHuman(n int64) string {
	if n < 0 {
		return "unknown"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

// ReplicaJoinSummary describes the initial physical copy of a new replica.
func ReplicaJoinSummary(join monitor.ReplicaJoin) string {
	if join.Progress == nil {
		return "join job running · base copy progress unavailable"
	}
	p := join.Progress
	phase := ValueOr(p.Phase, "base copy")
	summary := phase
	if fraction, ok := p.Fraction(); ok {
		summary += fmt.Sprintf(" · %.1f%% copied", 100*fraction)
	} else {
		summary += " · size estimate pending"
	}
	if join.RateBytesPerSec != nil {
		summary += " · " + Rate(*join.RateBytesPerSec)
	}
	if p.BackupTotal > 0 {
		summary += fmt.Sprintf(" (%s / ~%s)", BytesHuman(p.BackupStreamed), BytesHuman(p.BackupTotal))
	}
	return summary
}

// ReplicaJoinCompact fits the current copy percentage and speed in Overview.
func ReplicaJoinCompact(join monitor.ReplicaJoin) string {
	if join.Progress == nil {
		return "joining · progress unavailable"
	}
	summary := "base copy"
	if fraction, ok := join.Progress.Fraction(); ok {
		summary += fmt.Sprintf(" %.1f%%", 100*fraction)
	}
	if join.RateBytesPerSec != nil {
		summary += " · " + Rate(*join.RateBytesPerSec)
	}
	return summary
}

// BytesHumanU renders an unsigned byte count.
func BytesHumanU(n uint64) string {
	if n > math.MaxInt64 {
		return ">8 EiB"
	}
	return BytesHuman(int64(n))
}

// Spark renders a sparkline for a numeric series.
func Spark(v []float64) string {
	return spark(v, 1)
}

// SparkRange renders a sparkline scaled to the series itself, including values below 1.
func SparkRange(v []float64) string {
	return spark(v, 0)
}

func spark(v []float64, floor float64) string {
	if len(v) == 0 {
		return Muted.Render("collecting trend…")
	}
	maxV := floor
	for _, x := range v {
		if x > maxV {
			maxV = x
		}
	}
	if maxV == 0 {
		return Cyan.Render(strings.Repeat("▁", len(v)))
	}
	bars := "▁▂▃▄▅▆▇█"
	var b strings.Builder
	for _, x := range v {
		idx := int(x / maxV * 7)
		if idx > 7 {
			idx = 7
		}
		b.WriteRune([]rune(bars)[idx])
	}
	return Cyan.Render(b.String())
}

// ParseLSN parses a PostgreSQL log sequence number into a comparable integer.
func ParseLSN(s string) uint64 {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return 0
	}
	hi, e1 := strconv.ParseUint(parts[0], 16, 32)
	lo, e2 := strconv.ParseUint(parts[1], 16, 32)
	if e1 != nil || e2 != nil {
		return 0
	}
	return hi<<32 | lo
}

// LSNGap reports the byte gap between sent and replayed positions.
func LSNGap(sent, replay string) string {
	a, b := ParseLSN(sent), ParseLSN(replay)
	if a < b {
		return "0 B"
	}
	return BytesHuman(int64(a - b))
}

// ValueOr returns fallback when s is empty.
func ValueOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// Truncate shortens s to n runes, adding an ellipsis.
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}
