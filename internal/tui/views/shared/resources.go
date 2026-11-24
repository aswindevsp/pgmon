package shared

import (
	"strings"

	"postgres-monitor/internal/monitor"
)

// NodeName reports the Kubernetes node a pod runs on.
func NodeName(n Node) string {
	if n.Pod == nil {
		return "node unavailable"
	}
	return ValueOr(n.Pod.Spec.NodeName, "node pending")
}

// InstanceSignal summarizes WAL write activity for the primary and streaming,
// joining, or broken state for a replica. It is the compact one-line companion
// used by instance cards and trees.
func InstanceSignal(ctx Context, n Node) string {
	if n.Primary {
		if ctx.SQLLive && ctx.Data.Sql.WAL != nil {
			return Muted.Render("WAL ") + Spark(TrimHistory(ctx.Data.WalHistory, 8)) + Muted.Render(" ") + Cyan.Render(Rate(ctx.Data.WalRate))
		}
		return Muted.Render("WAL unavailable")
	}
	if r := ctx.Replication(n.Name); r != nil {
		state := Green.Render(r.State)
		if r.State != "streaming" {
			state = Amber.Render(r.State)
		}
		return state + Muted.Render(" "+ReplayLag(r))
	}
	for _, join := range monitor.ReplicaJoins(ctx.Data, ctx.SQLLive) {
		if join.Name == n.Name {
			return Amber.Render(ReplicaJoinCompact(join))
		}
	}
	if ctx.SQLLive {
		return Amber.Render("! No matching WAL sender")
	}
	return Muted.Render("? Replication unavailable")
}

// ResourceLines renders the CPU and memory meters for a single instance against
// its cap (pod limits, then requests, then node available with the node total
// in parens).
func ResourceLines(ctx Context, n Node, width int) []string {
	barW := max(6, min(12, width-28))
	usage, ok := ctx.Data.PodUsage[n.Name]
	hist := ctx.Data.PodUsageHistory[n.Name]
	hasUsage := ok
	if !hasUsage && (len(hist.CPU) > 0 || len(hist.Memory) > 0) {
		hasUsage = true
	}
	cap := monitor.ResolveEffectiveCap(n.Pod, ctx.Data.NodeCaps, ctx.Data.NodeUsage, usageForCap(usage, hist, ok), hasUsage)
	if !ok && len(hist.CPU) == 0 && len(hist.Memory) == 0 {
		note := Muted.Render("collecting…")
		if ctx.Data.MetricsErr != "" {
			note = Amber.Render("unavailable")
		}
		return []string{
			Muted.Render("CPU ") + Muted.Render(strings.Repeat("─", barW)) + "  " + note + Muted.Render(" / "+EffectiveCPUCap(cap)),
			Muted.Render("MEM ") + Muted.Render(strings.Repeat("─", barW)) + "  " + note + Muted.Render(" / "+EffectiveMemCap(cap)),
		}
	}
	usage = usageForCap(usage, hist, ok)
	return []string{
		ResourceRow("CPU", monitor.FormatCPU(usage.CPUCores), EffectiveCPUCap(cap), usage.CPUCores, cap.CPUCores, cap.HasCPU, barW),
		ResourceRow("MEM", monitor.FormatMem(usage.MemoryBytes), EffectiveMemCap(cap), float64(usage.MemoryBytes), float64(cap.MemoryBytes), cap.HasMemory, barW),
	}
}

// usageForCap fills a missing live sample from history so the available-cap
// math uses the same value shown as "used".
func usageForCap(usage monitor.ResourceUsage, hist monitor.UsageHistory, ok bool) monitor.ResourceUsage {
	if !ok {
		if len(hist.CPU) > 0 {
			usage.CPUCores = hist.CPU[len(hist.CPU)-1]
		}
		if len(hist.Memory) > 0 {
			usage.MemoryBytes = uint64(hist.Memory[len(hist.Memory)-1])
		}
	}
	return usage
}

// CPUCap renders a CPU ceiling, or an em dash when unknown.
func CPUCap(cap monitor.ResourceCap) string {
	if !cap.HasCPU {
		return "—"
	}
	return monitor.FormatCPU(cap.CPUCores)
}

// MemCap renders a memory ceiling, or an em dash when unknown.
func MemCap(cap monitor.ResourceCap) string {
	if !cap.HasMemory {
		return "—"
	}
	return monitor.FormatMem(cap.MemoryBytes)
}

// EffectiveCPUCap renders the denominator for a usage meter: pod limit when
// set, otherwise node available with the node total in parens.
func EffectiveCPUCap(cap monitor.EffectiveCap) string {
	if !cap.HasCPU {
		return "—"
	}
	s := monitor.FormatCPU(cap.CPUCores)
	if cap.CPUFromNode && cap.CPUAvailable && cap.HasNodeCPU {
		s += " (" + monitor.FormatCPU(cap.NodeCPUCores) + ")"
	}
	return s
}

// EffectiveMemCap renders the memory denominator, with node total in parens
// when falling back to node available.
func EffectiveMemCap(cap monitor.EffectiveCap) string {
	if !cap.HasMemory {
		return "—"
	}
	s := monitor.FormatMem(cap.MemoryBytes)
	if cap.MemFromNode && cap.MemAvailable && cap.HasNodeMemory {
		s += " (" + monitor.FormatMem(cap.NodeMemoryBytes) + ")"
	}
	return s
}

// ResourceRow draws one labeled usage meter with used and limit values.
func ResourceRow(label, used, limit string, usedN, limitN float64, hasLimit bool, barW int) string {
	bar := Muted.Render(strings.Repeat("─", barW))
	if hasLimit && limitN > 0 {
		bar = UsageMeter(usedN/limitN, barW)
	} else if hasLimit && usedN > 0 {
		// Node available exhausted: show a full bar rather than an empty one.
		bar = UsageMeter(1, barW)
	}
	return Muted.Render(label+" ") + bar + "  " + Bright.Render(used) + Muted.Render(" / "+limit)
}

// TrimHistory keeps the newest n samples.
func TrimHistory(v []float64, n int) []float64 {
	if n <= 0 || len(v) <= n {
		return v
	}
	return v[len(v)-n:]
}
