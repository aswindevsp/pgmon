package monitor

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const pgDataDir = "/var/lib/postgresql/data/pgdata"

// A carried rate is dropped once this long has passed without the live cursor
// moving: replay is then considered stalled rather than merely just after a
// container restart.
const replayCarryFor = 20 * time.Minute

// The manifest tail contains flat JSON objects; the WAL-Ranges entry carries
// the fixed backup end that determines the first consistent recovery state.
var manifestObjectPattern = regexp.MustCompile(`\{[^{}]*\}`)

// ParseLSNBytes parses a PostgreSQL LSN such as "19CC/5BE0F898" into bytes.
func ParseLSNBytes(s string) (uint64, bool) {
	parts := strings.Split(strings.TrimSpace(s), "/")
	if len(parts) != 2 {
		return 0, false
	}
	hi, errHi := strconv.ParseUint(parts[0], 16, 32)
	lo, errLo := strconv.ParseUint(parts[1], 16, 32)
	if errHi != nil || errLo != nil {
		return 0, false
	}
	return hi<<32 | lo, true
}

// ParseControlFile extracts the cluster state, latest checkpoint location,
// minimum recovery ending location, checkpoint timeline, and WAL segment size
// from pg_controldata output. The checkpoint location anchors the restartpoint
// cursor; the minimum recovery ending location is the live flush cursor that
// advances with replay. Both are returned, with the latter preferred for
// display; pg_wal is never consulted for replay progress.
func ParseControlFile(out []byte) (state string, position, live uint64, timeline int, segSize uint64, ok bool) {
	havePosition := false
	for _, line := range strings.Split(string(out), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Database cluster state":
			state = strings.TrimSpace(value)
		case "Latest checkpoint location":
			if parsed, valid := ParseLSNBytes(value); valid {
				position, havePosition = parsed, true
			}
		case "Minimum recovery ending location":
			if parsed, valid := ParseLSNBytes(value); valid {
				live = parsed
			}
		case "Latest checkpoint's TimeLineID":
			if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
				timeline = n
			}
		case "Bytes per WAL segment":
			if n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32); err == nil && n > 0 {
				segSize = n
			}
		}
	}
	if state == "" || !havePosition || timeline <= 0 {
		return "", 0, 0, 0, 0, false
	}
	return state, position, live, timeline, segSize, true
}

// ParseBackupManifestWALRanges reads the Start-LSN and End-LSN of the backup
// WAL range from the tail of a backup_manifest. The end is the fixed target
// PostgreSQL must replay past to reach a consistent state. When several ranges
// are present, the one ending furthest ahead wins.
func ParseBackupManifestWALRanges(out []byte) (start, end uint64, timeline int, ok bool) {
	for _, object := range manifestObjectPattern.FindAll(out, -1) {
		var entry struct {
			Timeline int    `json:"Timeline"`
			StartLSN string `json:"Start-LSN"`
			EndLSN   string `json:"End-LSN"`
		}
		if json.Unmarshal(object, &entry) != nil {
			continue
		}
		from, okStart := ParseLSNBytes(entry.StartLSN)
		to, okEnd := ParseLSNBytes(entry.EndLSN)
		if !okStart || !okEnd {
			continue
		}
		if !ok || to > end {
			start, end, timeline, ok = from, to, entry.Timeline, true
		}
	}
	return start, end, timeline, ok
}

// ReplayCandidates returns the pods whose archive WAL replay should be probed:
// running instances with no WAL sender and no active base copy. Ready pods
// are included: CNPG can report Ready while a replica is still replaying WAL
// from the archive and has not yet connected to the primary.
func ReplayCandidates(s Snapshot) []PodResource {
	joins := ReplicaJoins(s, true)
	senders := make(map[string]bool, len(s.Sql.Replication))
	for _, r := range s.Sql.Replication {
		senders[r.ApplicationName] = true
	}
	candidates := make([]PodResource, 0)
	for _, pod := range s.Pods {
		name := pod.Metadata.Name
		if name == "" || name == s.SqlPrimary || name == s.Cluster.Status.CurrentPrimary {
			continue
		}
		ownedByJob := false
		for _, owner := range pod.Metadata.OwnerReferences {
			if owner.Kind == "Job" {
				ownedByJob = true
				break
			}
		}
		if ownedByJob || pod.Status.Phase != "Running" || len(pod.Status.ContainerStatuses) == 0 {
			continue
		}
		if senders[name] {
			continue
		}
		baseCopy := false
		for _, join := range joins {
			if join.Name == name {
				baseCopy = true
				break
			}
		}
		if baseCopy {
			continue
		}
		candidates = append(candidates, pod)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Metadata.Name < candidates[j].Metadata.Name })
	return candidates
}

// replayRestarts reports the postgres container's restarts, falling back to the
// pod total when container names are unavailable.
func replayRestarts(pod PodResource) int32 {
	total := int32(0)
	for _, c := range pod.Status.ContainerStatuses {
		if c.Name == "postgres" {
			return int32(c.RestartCount)
		}
		total += int32(c.RestartCount)
	}
	return total
}

// replayLive is the observed replay cursor: the control file's minimum
// recovery ending location, with the restartpoint as a lower-bound fallback.
func replayLive(p WALReplayProgress) uint64 {
	if p.Live > p.Position {
		return p.Live
	}
	return p.Position
}

// replayRate derives the effective rate from two live-cursor samples. The
// minimum recovery ending location moves with replay on every healthy sample,
// so a fresh measurement uses the elapsed time since the last move. A restart
// rewinds the cursor and must not produce a delta; a recent rate is carried
// forward instead. advanced reports whether this sample measured movement,
// carried whether an old rate is reused.
func replayRate(previous, current WALReplayProgress) (rate float64, advanced, carried bool) {
	samePod := previous.Pod != "" && previous.Pod == current.Pod
	if samePod && previous.Restarts == current.Restarts && !previous.At.IsZero() && !current.At.IsZero() {
		anchorAt := previous.LastAdvance
		if anchorAt.IsZero() {
			anchorAt = previous.At
		}
		from, to := replayLive(previous), replayLive(current)
		if to > from && !anchorAt.After(current.At) {
			if elapsed := current.At.Sub(anchorAt); elapsed > 0 {
				return float64(to-from) / elapsed.Seconds(), true, false
			}
		}
	}
	if samePod && previous.HasRate && !previous.LastAdvance.IsZero() &&
		current.At.Sub(previous.LastAdvance) <= replayCarryFor {
		return previous.RateBytesPerSec, false, true
	}
	return 0, false, false
}

// applyReplayRate updates a fresh sample with the rate and advance timestamp,
// keeping the last advance time when the rate is merely carried forward.
func applyReplayRate(previous, current WALReplayProgress) WALReplayProgress {
	rate, advanced, carried := replayRate(previous, current)
	switch {
	case advanced:
		current.RateBytesPerSec, current.HasRate, current.LastAdvance = rate, true, current.At
	case carried:
		current.RateBytesPerSec, current.HasRate, current.LastAdvance = rate, true, previous.LastAdvance
	default:
		current.RateBytesPerSec, current.HasRate, current.LastAdvance = 0, false, time.Time{}
	}
	return current
}

// CollectReplayProgress refreshes archive replay progress for starting
// replicas through read-only probes: pg_controldata gives the state, live
// minimum recovery ending location, restartpoint anchor, timeline, and segment
// size, while the backup manifest fixes the consistency target. pg_wal is NOT
// read: CNPG's parallel restore prefetches segments there ahead of replay, so
// its newest file would badly overstate the consumed position. It is best
// effort: failed or unparsable probes only omit that instance.
func CollectReplayProgress(cfg Config, s *Snapshot) {
	if s.SqlErr != "" || s.SqlAt.IsZero() {
		s.ReplayProgress = map[string]WALReplayProgress{}
		return
	}
	progress := map[string]WALReplayProgress{}
	for _, pod := range ReplayCandidates(*s) {
		name := pod.Metadata.Name
		previous, seen := s.ReplayProgress[name]
		out, err := KubectlExec(cfg, 8*time.Second, "exec", name, "-c", "postgres", "--", "pg_controldata", pgDataDir)
		if err != nil {
			continue
		}
		state, position, live, timeline, segSize, ok := ParseControlFile(out)
		if !ok || !strings.Contains(strings.ToLower(state), "recovery") {
			continue
		}
		if live == 0 {
			// The minimum recovery ending location is preferred; the
			// restartpoint keeps a usable cursor if that field is absent.
			live = position
		}
		// The backup manifest never changes: keep the parsed target across cycles.
		start, target := previous.Start, previous.Target
		if start == 0 || target == 0 {
			if manifest, err := KubectlExec(cfg, 8*time.Second, "exec", name, "-c", "postgres", "--", "tail", "-n", "12", pgDataDir+"/backup_manifest"); err == nil {
				if parsedStart, parsedTarget, _, manifestOK := ParseBackupManifestWALRanges(manifest); manifestOK {
					start, target = parsedStart, parsedTarget
				}
			}
		}
		if segSize == 0 {
			segSize = previous.SegmentSize
		}
		now := time.Now()
		current := WALReplayProgress{
			Pod:         name,
			State:       state,
			Timeline:    timeline,
			Position:    position,
			Live:        live,
			LiveAt:      now,
			Start:       start,
			Target:      target,
			SegmentSize: segSize,
			Restarts:    replayRestarts(pod),
			At:          now,
		}
		if seen {
			current = applyReplayRate(previous, current)
		}
		progress[name] = current
	}
	s.ReplayProgress = progress
}
