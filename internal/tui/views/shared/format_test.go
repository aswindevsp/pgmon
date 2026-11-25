package shared

import (
	"strings"
	"testing"
	"time"

	"pgmon/internal/monitor"
)

func replayContext() Context {
	var ctx Context
	ctx.SQLLive = true
	ctx.Data.SqlAt = time.Now()
	ctx.Data.Sql.TimelineID = 33
	return ctx
}

func replayPod() monitor.WALReplayProgress {
	return monitor.WALReplayProgress{
		Pod:             "orders-db-3",
		State:           "in archive recovery",
		Timeline:        33,
		Position:        512 << 10,
		Start:           256 << 10,
		Target:          1024 << 10,
		Restarts:        2,
		RateBytesPerSec: 55.3 * 1024 * 1024,
		HasRate:         true,
	}
}

func TestLSNHex(t *testing.T) {
	value, ok := monitor.ParseLSNBytes("19CC/5BE0F898")
	if !ok {
		t.Fatal("fixture LSN should parse")
	}
	for input, want := range map[uint64]string{
		value: "19CC/5BE0F898",
		0:     "0/00000000",
		0x1A:  "0/0000001A",
	} {
		if got := LSNHex(input); got != want {
			t.Errorf("LSNHex(%d) = %q, want %q", input, got, want)
		}
	}
}

func TestWALReplayETA(t *testing.T) {
	p := monitor.WALReplayProgress{HasRate: true, RateBytesPerSec: 42.3 * 1024 * 1024}
	behind := int64(p.RateBytesPerSec * 3900)
	if got, want := WALReplayETA(p, behind), "ETA ≈1h5m0s"; got != want {
		t.Fatalf("ETA = %q, want %q", got, want)
	}
	if got, want := WALReplayETA(p, int64(p.RateBytesPerSec*65)), "ETA ≈1m5s"; got != want {
		t.Fatalf("rounded ETA = %q, want %q", got, want)
	}
	for _, tc := range []struct {
		name string
		p    monitor.WALReplayProgress
		left int64
	}{
		{"no remaining bytes", p, 0},
		{"negative remaining", p, -1},
		{"no rate", monitor.WALReplayProgress{}, behind},
		{"zero rate", monitor.WALReplayProgress{HasRate: true}, behind},
	} {
		if got := WALReplayETA(tc.p, tc.left); got != "" {
			t.Errorf("%s: ETA = %q, want empty", tc.name, got)
		}
	}
}

func TestDurationETA(t *testing.T) {
	for input, want := range map[time.Duration]string{
		42 * time.Second:             "42s",
		12 * time.Minute:             "12m",
		3*time.Hour + 5*time.Minute:  "3h05m",
		2*24*time.Hour + 4*time.Hour: "2d04h",
	} {
		if got := DurationETA(input); got != want {
			t.Errorf("DurationETA(%s) = %q, want %q", input, got, want)
		}
	}
}

func TestWALReplayIdle(t *testing.T) {
	ctx, p := replayContext(), replayPod()
	p.Live = p.Position
	if _, ok := WALReplayIdle(ctx, p); ok {
		t.Fatal("a missing live-cursor sample must not report idle")
	}
	p.LiveAt = time.Now().Add(-4*time.Minute - 59*time.Second)
	if _, ok := WALReplayIdle(ctx, p); ok {
		t.Fatal("4m59s without a segment must not report idle")
	}
	p.LiveAt = time.Now().Add(-5*time.Minute - time.Second)
	if idle, ok := WALReplayIdle(ctx, p); !ok || idle < 5*time.Minute {
		t.Fatalf("5m01s without a segment = %s, %v; want idle", idle, ok)
	}
	p.Live = p.Target
	if _, ok := WALReplayIdle(ctx, p); ok {
		t.Fatal("a replica at its consistency target must not report idle")
	}
	p.Live = p.Position
	p.Target = 0
	if idle, ok := WALReplayIdle(ctx, p); !ok || idle < 5*time.Minute {
		t.Fatalf("unknown target should still report idle: %s, %v", idle, ok)
	}
	p.Target = 1024 << 10
	p.Live = p.Target
	ctx.Data.Sql.CurrentWALLSN = "0/00200000"
	if idle, ok := WALReplayIdle(ctx, p); !ok || idle < 5*time.Minute {
		t.Fatalf("stalled past backup end but behind primary = %s, %v; want idle", idle, ok)
	}
}

func TestWALReplaySummaryReportsIdleLive(t *testing.T) {
	ctx, p := replayContext(), replayPod()
	p.Live, p.LiveAt = p.Position, time.Now().Add(-6*time.Minute)
	summary := WALReplaySummary(ctx, p)
	if !strings.Contains(summary, "no WAL restored for 6m") {
		t.Fatalf("summary missing idle clause: %q", summary)
	}
	if got := WALReplayCompact(ctx, p); strings.Contains(got, "no WAL restored") {
		t.Fatalf("compact should stay terse: %q", got)
	}
}

func TestWALReplayCatchupProgressUsesPrimaryWhenAhead(t *testing.T) {
	ctx, p := replayContext(), replayPod()
	// Backup end 1 MiB, live 512 KiB, start 256 KiB → 33.3% of consistency.
	c := WALReplayCatchupProgress(ctx, p)
	if !c.OK || c.ToPrimary || !c.HasFraction || c.Reached {
		t.Fatalf("backup-end catchup = %+v", c)
	}
	if c.Remaining != 512<<10 {
		t.Fatalf("remaining = %d, want 512 KiB", c.Remaining)
	}
	if got := WALReplayMeterLine(ctx, p, 40); !strings.Contains(got, "~33.3%") {
		t.Fatalf("backup-end meter = %q", got)
	}
	// Primary current WAL past the backup end is the catch-up goal.
	ctx.Data.Sql.CurrentWALLSN = "0/00200000"
	c = WALReplayCatchupProgress(ctx, p)
	if !c.OK || !c.ToPrimary || !c.HasFraction || c.Reached {
		t.Fatalf("primary catchup = %+v", c)
	}
	if c.Remaining != 0x00200000-(512<<10) {
		t.Fatalf("remaining behind primary = %d", c.Remaining)
	}
	if got := WALReplayCompact(ctx, p); !strings.Contains(got, "behind primary") {
		t.Fatalf("compact should name the primary: %q", got)
	}
	if got := WALReplayMeterLine(ctx, p, 40); !strings.Contains(got, "~") || strings.Contains(got, "100%") {
		t.Fatalf("in-progress primary meter = %q", got)
	}
	p.Position, p.Live = 0x00200000, 0x00200000
	c = WALReplayCatchupProgress(ctx, p)
	if !c.Reached || c.Fraction != 1 {
		t.Fatalf("caught up = %+v", c)
	}
	if got := WALReplayMeterLine(ctx, p, 40); !strings.Contains(got, "caught up with primary") {
		t.Fatalf("reached primary meter = %q", got)
	}
}

func TestWALReplayCompactAndSummary(t *testing.T) {
	ctx, p := replayContext(), replayPod()
	if got, want := WALReplayCompact(ctx, p), "catching up · ~512.0 KiB to consistency · ETA ≈<1s"; got != want {
		t.Fatalf("compact = %q, want %q", got, want)
	}
	if got, want := WALReplaySummary(ctx, p), "replaying WAL from archive · at 0/00080000 · ~512.0 KiB to consistency · 55.3 MiB/s · ETA ≈<1s · 2 restarts"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
	p.HasRate = false
	p.Restarts = 0
	if got, want := WALReplayCompact(ctx, p), "catching up · ~512.0 KiB to consistency"; got != want {
		t.Fatalf("compact without rate = %q, want %q", got, want)
	}
	if got, want := WALReplaySummary(ctx, p), "replaying WAL from archive · at 0/00080000 · ~512.0 KiB to consistency"; got != want {
		t.Fatalf("summary without rate = %q, want %q", got, want)
	}
}

func TestWALReplayOmitsRemainingWhenUnavailable(t *testing.T) {
	ctx, p := replayContext(), replayPod()
	p.Timeline = 34
	if behind, ok := WALReplayBehind(ctx, p); ok || behind != 0 {
		t.Fatalf("timeline mismatch produced a remaining estimate: %d, %v", behind, ok)
	}
	if got, want := WALReplayCompact(ctx, p), "catching up · replaying WAL from archive · 55.3 MiB/s"; got != want {
		t.Fatalf("compact with timeline mismatch = %q, want %q", got, want)
	}
	if got, want := WALReplaySummary(ctx, p), "replaying WAL from archive · at 0/00080000 · 55.3 MiB/s · 2 restarts"; got != want {
		t.Fatalf("summary with timeline mismatch = %q, want %q", got, want)
	}
	p.Timeline = ctx.Data.Sql.TimelineID
	p.Target = 0
	if _, ok := WALReplayBehind(ctx, p); ok {
		t.Fatal("missing manifest target should leave the estimate unavailable")
	}
	ctx.SQLLive = false
	if got, want := WALReplaySummary(ctx, p), "replaying WAL from archive · at 0/00080000 · 55.3 MiB/s · 2 restarts"; got != want {
		t.Fatalf("summary without manifest = %q, want %q", got, want)
	}
	p.Target = 1024 << 10
	ctx.SQLLive = false
	if behind, ok := WALReplayBehind(ctx, p); !ok || behind != 512<<10 {
		t.Fatalf("target math should not need live SQL: %d, %v", behind, ok)
	}
	p.Position = p.Target
	if behind, ok := WALReplayBehind(ctx, p); !ok || behind != 0 {
		t.Fatalf("position at target = %d, %v; want 0, true", behind, ok)
	}
	if got := WALReplayCompact(ctx, p); got != "catching up · ~0.0 B to consistency · 55.3 MiB/s" {
		t.Fatalf("compact at target should omit ETA: %q", got)
	}
	if got := WALReplaySummary(ctx, p); got != "replaying WAL from archive · at 0/00100000 · ~0.0 B to consistency · 55.3 MiB/s · 2 restarts" {
		t.Fatalf("summary at target should omit ETA: %q", got)
	}
}
