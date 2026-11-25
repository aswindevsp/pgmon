package shared

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"pgmon/internal/monitor"
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

// LSNHex renders a byte offset as a PostgreSQL LSN, for example 19CC/5BE0F898:
// the high half without unnecessary leading zeros, then the low half as eight
// uppercase hex digits.
func LSNHex(pos uint64) string {
	return fmt.Sprintf("%X/%08X", pos>>32, pos&0xFFFFFFFF)
}

// liveProjectionFor bounds how long a display may advance the position beyond
// its last sample: enough to smooth between five-second ticks, short enough
// that a stale sample is never projected onto the consistency target.
const liveProjectionFor = 30 * time.Second

// WALReplayLivePosition estimates the replay position at display time: the
// control file's minimum recovery ending location (live flush cursor, with the
// restartpoint as a lower-bound fallback) advanced by the measured rate since
// that sample, never past the next unfetched segment or the consistency target.
func WALReplayLivePosition(p monitor.WALReplayProgress) uint64 {
	base := max(p.Position, p.Live)
	if base == 0 || !p.HasRate || p.RateBytesPerSec <= 0 || p.LiveAt.IsZero() {
		return base
	}
	age := time.Since(p.LiveAt)
	if age <= 0 || age > liveProjectionFor {
		return base
	}
	projected := float64(base) + p.RateBytesPerSec*age.Seconds()
	if p.SegmentSize > 0 {
		if limit := float64(base) + float64(p.SegmentSize); projected > limit {
			projected = limit
		}
	}
	if p.Target > base && projected > float64(p.Target) {
		projected = float64(p.Target)
	}
	if projected >= float64(math.MaxUint64) {
		return base
	}
	if projected <= float64(base) {
		return base
	}
	return uint64(projected)
}

// WALReplayBehind estimates the bytes left before the replica reaches its
// first consistent state: the backup end minus the live replay position. The
// fixed target comes from the backup manifest, so no primary SQL sample is
// needed; a mismatched timeline makes the estimate unusable.
func WALReplayBehind(ctx Context, p monitor.WALReplayProgress) (int64, bool) {
	if p.Target == 0 {
		return 0, false
	}
	if p.Timeline > 0 && ctx.Data.Sql.TimelineID > 0 && p.Timeline != ctx.Data.Sql.TimelineID {
		return 0, false
	}
	position := WALReplayLivePosition(p)
	if position >= p.Target {
		return 0, true
	}
	remaining := p.Target - position
	if remaining > uint64(math.MaxInt64) {
		return math.MaxInt64, true
	}
	return int64(remaining), true
}

// WALReplayCatchup is progress from the backup start toward the replica's
// catch-up goal: the primary's current WAL when that is ahead of the backup
// end, otherwise the backup end that marks the first consistent recovery state.
type WALReplayCatchup struct {
	Fraction    float64
	HasFraction bool
	Remaining   uint64
	Target      uint64
	ToPrimary   bool
	Reached     bool
	OK          bool
}

// WALReplayCatchupProgress computes the catch-up meter. A percentage needs a
// backup start below the target; remaining bytes are still reported without one.
func WALReplayCatchupProgress(ctx Context, p monitor.WALReplayProgress) WALReplayCatchup {
	target, toPrimary := replayCatchupTarget(ctx, p)
	if target == 0 {
		return WALReplayCatchup{}
	}
	live := WALReplayLivePosition(p)
	out := WALReplayCatchup{Target: target, ToPrimary: toPrimary, OK: true}
	if live >= target {
		out.Fraction, out.HasFraction, out.Reached = 1, true, true
		return out
	}
	out.Remaining = target - live
	if p.Start > 0 && p.Start < target {
		fraction := (float64(live) - float64(p.Start)) / float64(target-p.Start)
		out.Fraction = math.Min(1, math.Max(0, fraction))
		out.HasFraction = true
	}
	return out
}

func replayCatchupTarget(ctx Context, p monitor.WALReplayProgress) (uint64, bool) {
	timelineOK := p.Timeline == 0 || ctx.Data.Sql.TimelineID == 0 || p.Timeline == ctx.Data.Sql.TimelineID
	if timelineOK {
		if primary, ok := monitor.ParseLSNBytes(ctx.Data.Sql.CurrentWALLSN); ok && primary > p.Target {
			return primary, true
		}
	}
	if p.Target == 0 {
		return 0, false
	}
	if p.Timeline > 0 && ctx.Data.Sql.TimelineID > 0 && p.Timeline != ctx.Data.Sql.TimelineID {
		return 0, false
	}
	return p.Target, false
}

// WALReplayMeterLine is the shared catch-up bar used on Overview, Replication,
// and Pods. In-progress fill always leaves an empty end cell and stays under
// 100%; only a reached target fills the bar.
func WALReplayMeterLine(ctx Context, p monitor.WALReplayProgress, width int) string {
	c := WALReplayCatchupProgress(ctx, p)
	if !c.OK || (!c.HasFraction && !c.Reached) {
		return ""
	}
	barWidth := max(8, min(32, width-18))
	if c.Reached {
		label := "100% · backup end reached"
		if c.ToPrimary {
			label = "100% · caught up with primary"
		}
		return Meter(1, barWidth) + "  " + Green.Render(label)
	}
	barFraction := math.Min(c.Fraction, float64(barWidth-1)/float64(barWidth))
	return ColoredMeter(barFraction, barWidth, Amber) + fmt.Sprintf("  ~%.1f%%", math.Min(99.9, c.Fraction*100))
}

func walReplayRemainingLabel(c WALReplayCatchup) string {
	if c.ToPrimary {
		return "~" + BytesHumanU(c.Remaining) + " behind primary"
	}
	return "~" + BytesHumanU(c.Remaining) + " to consistency"
}

// DurationETA renders a coarse elapsed or remaining label: 42s, 12m, 3h05m, 2d04h.
func DurationETA(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", max(1, int(d.Seconds())))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Round(time.Minute).Minutes()))
	}
	if d < 48*time.Hour {
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd%02dh", int(d.Hours())/24, int(d.Hours())%24)
}

// WALReplayETA estimates the time left before consistency from the remaining
// bytes and the measured replay rate. It stays empty when either is unknown.
func WALReplayETA(p monitor.WALReplayProgress, behind int64) string {
	if behind <= 0 || !p.HasRate || p.RateBytesPerSec <= 0 {
		return ""
	}
	seconds := float64(behind) / p.RateBytesPerSec
	return "ETA ≈" + Duration(time.Duration(seconds*float64(time.Second)))
}

// WALReplayIdle reports how long the control-file live cursor has stood still.
// A healthy replay moves it every sample, so more than five minutes without
// movement is a genuine stall rather than restartpoint granularity.
func WALReplayIdle(ctx Context, p monitor.WALReplayProgress) (time.Duration, bool) {
	if p.LiveAt.IsZero() {
		return 0, false
	}
	c := WALReplayCatchupProgress(ctx, p)
	if c.OK && c.Reached {
		return 0, false
	}
	if !c.ToPrimary && p.Target > 0 && p.Live >= p.Target {
		return 0, false
	}
	idle := time.Since(p.LiveAt)
	if idle <= 5*time.Minute {
		return 0, false
	}
	return idle, true
}

func walReplayRemaining(ctx Context, p monitor.WALReplayProgress) (int64, bool, string) {
	if c := WALReplayCatchupProgress(ctx, p); c.OK {
		remaining := int64(c.Remaining)
		if c.Remaining > uint64(math.MaxInt64) {
			remaining = math.MaxInt64
		}
		return remaining, true, walReplayRemainingLabel(c)
	}
	behind, ok := WALReplayBehind(ctx, p)
	if !ok {
		return 0, false, ""
	}
	return behind, true, "~" + BytesHumanU(uint64(behind)) + " to consistency"
}

// WALReplayCompact is the one-line archive replay state for topology links.
func WALReplayCompact(ctx Context, p monitor.WALReplayProgress) string {
	parts := []string{"catching up"}
	behind, ok, label := walReplayRemaining(ctx, p)
	if ok {
		parts = append(parts, label)
	} else {
		parts = append(parts, "replaying WAL from archive")
	}
	if eta := WALReplayETA(p, behind); eta != "" {
		parts = append(parts, eta)
	} else if p.HasRate {
		parts = append(parts, Rate(p.RateBytesPerSec))
	}
	return strings.Join(parts, " · ")
}

// WALReplaySummary is the detailed archive replay state for pod details and
// the replication dashboard.
func WALReplaySummary(ctx Context, p monitor.WALReplayProgress) string {
	parts := []string{"replaying WAL from archive"}
	if position := WALReplayLivePosition(p); position > 0 {
		parts = append(parts, "at "+LSNHex(position))
	}
	behind, ok, label := walReplayRemaining(ctx, p)
	if ok {
		parts = append(parts, label)
	}
	if p.HasRate {
		parts = append(parts, Rate(p.RateBytesPerSec))
	}
	if eta := WALReplayETA(p, behind); eta != "" {
		parts = append(parts, eta)
	}
	if idle, ok := WALReplayIdle(ctx, p); ok {
		parts = append(parts, "no WAL restored for "+DurationETA(idle))
	}
	if p.Restarts > 0 {
		parts = append(parts, fmt.Sprintf("%d restarts", p.Restarts))
	}
	return strings.Join(parts, " · ")
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
