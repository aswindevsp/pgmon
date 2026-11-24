package monitor

import (
	"strconv"
	"strings"
)

// ResourceUsage is a live CPU and memory sample from metrics.k8s.io.
type ResourceUsage struct {
	CPUCores    float64
	MemoryBytes uint64
}

type metricsList struct {
	Items []metricsItem `json:"items"`
}

type metricsItem struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Usage      resourceQuantity `json:"usage"`
	Containers []struct {
		Usage resourceQuantity `json:"usage"`
	} `json:"containers"`
}

type resourceQuantity struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

func collectUsage(cfg Config, s *Snapshot) {
	type sample struct {
		list    metricsList
		err     error
		podList bool
	}
	results := make(chan sample, 2)
	go func() {
		var list metricsList
		err := GetJSON(cfg, &list, "get", "--raw", "/apis/metrics.k8s.io/v1beta1/namespaces/"+cfg.Namespace+"/pods")
		results <- sample{list: list, err: err, podList: true}
	}()
	go func() {
		var list metricsList
		err := GetJSON(cfg, &list, "get", "--raw", "/apis/metrics.k8s.io/v1beta1/nodes")
		results <- sample{list: list, err: err, podList: false}
	}()
	var podErr, nodeErr error
	for range 2 {
		got := <-results
		if got.podList {
			podErr = got.err
			if got.err == nil {
				s.PodUsage = usageByName(got.list, true)
				s.PodUsageHistory = appendUsageHistories(s.PodUsageHistory, s.PodUsage, 12)
			}
		} else {
			nodeErr = got.err
			if got.err == nil {
				s.NodeUsage = usageByName(got.list, false)
				s.NodeUsageHistory = appendUsageHistories(s.NodeUsageHistory, s.NodeUsage, 12)
			}
		}
	}
	switch {
	case podErr != nil && nodeErr != nil:
		s.MetricsErr = podErr.Error()
	case podErr != nil:
		s.MetricsErr = podErr.Error()
	case nodeErr != nil:
		s.MetricsErr = nodeErr.Error()
	default:
		s.MetricsErr = ""
	}
}

func appendUsageHistories(previous map[string]UsageHistory, current map[string]ResourceUsage, limit int) map[string]UsageHistory {
	next := make(map[string]UsageHistory, len(current))
	for name, usage := range current {
		old := previous[name]
		next[name] = UsageHistory{
			CPU:    AppendHistory(old.CPU, usage.CPUCores, limit),
			Memory: AppendHistory(old.Memory, float64(usage.MemoryBytes), limit),
		}
	}
	return next
}

func usageByName(list metricsList, sumContainers bool) map[string]ResourceUsage {
	out := make(map[string]ResourceUsage, len(list.Items))
	for _, item := range list.Items {
		usage := item.Usage
		if sumContainers {
			usage = resourceQuantity{}
			for _, c := range item.Containers {
				usage.CPU = addCPU(usage.CPU, c.Usage.CPU)
				usage.Memory = addMemory(usage.Memory, c.Usage.Memory)
			}
		}
		cpu, okCPU := ParseCPUQuantity(usage.CPU)
		mem, okMem := ParseMemoryQuantity(usage.Memory)
		if !okCPU && !okMem {
			continue
		}
		out[item.Metadata.Name] = ResourceUsage{CPUCores: cpu, MemoryBytes: mem}
	}
	return out
}

func addCPU(a, b string) string {
	va, okA := ParseCPUQuantity(a)
	vb, okB := ParseCPUQuantity(b)
	if !okA && !okB {
		return ""
	}
	return strconv.FormatFloat(va+vb, 'f', -1, 64)
}

func addMemory(a, b string) string {
	va, okA := ParseMemoryQuantity(a)
	vb, okB := ParseMemoryQuantity(b)
	if !okA && !okB {
		return ""
	}
	return strconv.FormatUint(va+vb, 10)
}

// ParseCPUQuantity converts a Kubernetes CPU quantity into cores.
func ParseCPUQuantity(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	scale := 1.0
	switch {
	case strings.HasSuffix(s, "n"):
		s, scale = strings.TrimSuffix(s, "n"), 1e-9
	case strings.HasSuffix(s, "u"):
		s, scale = strings.TrimSuffix(s, "u"), 1e-6
	case strings.HasSuffix(s, "m"):
		s, scale = strings.TrimSuffix(s, "m"), 1e-3
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v * scale, true
}

// ParseMemoryQuantity converts a Kubernetes memory quantity into bytes.
func ParseMemoryQuantity(s string) (uint64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	type suffix struct {
		text  string
		bytes uint64
	}
	for _, unit := range []suffix{
		{"Ei", 1 << 60}, {"Pi", 1 << 50}, {"Ti", 1 << 40}, {"Gi", 1 << 30}, {"Mi", 1 << 20}, {"Ki", 1 << 10},
		{"E", 1e18}, {"P", 1e15}, {"T", 1e12}, {"G", 1e9}, {"M", 1e6}, {"k", 1e3},
	} {
		if strings.HasSuffix(s, unit.text) {
			v, err := strconv.ParseFloat(strings.TrimSuffix(s, unit.text), 64)
			if err != nil || v < 0 {
				return 0, false
			}
			return uint64(v * float64(unit.bytes)), true
		}
	}
	v, err := strconv.ParseUint(s, 10, 64)
	return v, err == nil
}

// FormatCPU renders cores as millicores below 1, otherwise as cores.
func FormatCPU(cores float64) string {
	if cores < 1 {
		return strconv.FormatFloat(cores*1000, 'f', 0, 64) + "m"
	}
	if cores == float64(int64(cores)) {
		return strconv.FormatInt(int64(cores), 10)
	}
	return strconv.FormatFloat(cores, 'f', 2, 64)
}

// FormatMem renders bytes as a compact Kubernetes-style quantity.
func FormatMem(bytes uint64) string {
	switch {
	case bytes >= 1<<30 && bytes%(1<<30) == 0:
		return strconv.FormatUint(bytes>>30, 10) + "Gi"
	case bytes >= 1<<30:
		return strconv.FormatFloat(float64(bytes)/float64(1<<30), 'f', 1, 64) + "Gi"
	case bytes >= 1<<20:
		return strconv.FormatUint(bytes>>20, 10) + "Mi"
	case bytes >= 1<<10:
		return strconv.FormatUint(bytes>>10, 10) + "Ki"
	default:
		return strconv.FormatUint(bytes, 10) + "B"
	}
}

// ResourceCap is the CPU and memory ceiling used to graph usage: limits when
// set, otherwise requests.
type ResourceCap struct {
	CPUCores    float64
	MemoryBytes uint64
	HasCPU      bool
	HasMemory   bool
}

// PodResourceCap sums container limits, falling back to requests when a limit is absent.
func PodResourceCap(p *PodResource) ResourceCap {
	var cap ResourceCap
	if p == nil {
		return cap
	}
	var reqCPU float64
	var reqMem uint64
	var hasReqCPU, hasReqMem bool
	for _, c := range p.Spec.Containers {
		if v, ok := ParseCPUQuantity(c.Resources.Limits["cpu"]); ok {
			cap.CPUCores += v
			cap.HasCPU = true
		} else if v, ok := ParseCPUQuantity(c.Resources.Requests["cpu"]); ok {
			reqCPU += v
			hasReqCPU = true
		}
		if v, ok := ParseMemoryQuantity(c.Resources.Limits["memory"]); ok {
			cap.MemoryBytes += v
			cap.HasMemory = true
		} else if v, ok := ParseMemoryQuantity(c.Resources.Requests["memory"]); ok {
			reqMem += v
			hasReqMem = true
		}
	}
	if !cap.HasCPU && hasReqCPU {
		cap.CPUCores, cap.HasCPU = reqCPU, true
	}
	if !cap.HasMemory && hasReqMem {
		cap.MemoryBytes, cap.HasMemory = reqMem, true
	}
	return cap
}

// NodeResourceCap reads allocatable CPU and memory, falling back to capacity.
func NodeResourceCap(n NodeResource) ResourceCap {
	qty := n.Status.Allocatable
	if len(qty) == 0 {
		qty = n.Status.Capacity
	}
	var cap ResourceCap
	if v, ok := ParseCPUQuantity(qty["cpu"]); ok {
		cap.CPUCores, cap.HasCPU = v, true
	}
	if v, ok := ParseMemoryQuantity(qty["memory"]); ok {
		cap.MemoryBytes, cap.HasMemory = v, true
	}
	return cap
}

func nodeCapsByName(nodes []NodeResource) map[string]ResourceCap {
	out := make(map[string]ResourceCap, len(nodes))
	for _, n := range nodes {
		if cap := NodeResourceCap(n); cap.HasCPU || cap.HasMemory {
			out[n.Metadata.Name] = cap
		}
	}
	return out
}

// ResolveResourceCap uses pod limits or requests, then the node's allocatable.
func ResolveResourceCap(p *PodResource, nodeCaps map[string]ResourceCap) ResourceCap {
	cap := PodResourceCap(p)
	if p == nil {
		return cap
	}
	node := nodeCaps[p.Spec.NodeName]
	if !cap.HasCPU && node.HasCPU {
		cap.CPUCores, cap.HasCPU = node.CPUCores, true
	}
	if !cap.HasMemory && node.HasMemory {
		cap.MemoryBytes, cap.HasMemory = node.MemoryBytes, true
	}
	return cap
}

// EffectiveCap is the denominator used to graph usage. When a pod has no
// limit (or request), the denominator is the node's allocatable minus what
// other workloads on the same node are using, with the node total kept for
// display in parens.
type EffectiveCap struct {
	CPUCores        float64
	MemoryBytes     uint64
	HasCPU          bool
	HasMemory       bool
	CPUFromNode     bool
	MemFromNode     bool
	NodeCPUCores    float64
	NodeMemoryBytes uint64
	HasNodeCPU      bool
	HasNodeMemory   bool
	CPUAvailable    bool
	MemAvailable    bool
}

// ResolveEffectiveCap uses pod limits or requests, then the node's available
// capacity (allocatable minus other pods on the same node). podUsage should be
// the same sample shown as "used"; hasPodUsage reports whether it is known.
func ResolveEffectiveCap(p *PodResource, nodeCaps map[string]ResourceCap, nodeUsage map[string]ResourceUsage, podUsage ResourceUsage, hasPodUsage bool) EffectiveCap {
	var out EffectiveCap
	if p == nil {
		return out
	}
	podCap := PodResourceCap(p)
	if podCap.HasCPU {
		out.CPUCores, out.HasCPU = podCap.CPUCores, true
	} else if node, ok := nodeCaps[p.Spec.NodeName]; ok && node.HasCPU {
		out.CPUFromNode = true
		out.NodeCPUCores, out.HasNodeCPU = node.CPUCores, true
		out.CPUCores, out.HasCPU = node.CPUCores, true
		if hasPodUsage {
			if nu, ok := nodeUsage[p.Spec.NodeName]; ok {
				avail := node.CPUCores - (nu.CPUCores - podUsage.CPUCores)
				if avail < 0 {
					avail = 0
				}
				out.CPUCores = avail
				out.CPUAvailable = true
			}
		}
	}
	if podCap.HasMemory {
		out.MemoryBytes, out.HasMemory = podCap.MemoryBytes, true
	} else if node, ok := nodeCaps[p.Spec.NodeName]; ok && node.HasMemory {
		out.MemFromNode = true
		out.NodeMemoryBytes, out.HasNodeMemory = node.MemoryBytes, true
		out.MemoryBytes, out.HasMemory = node.MemoryBytes, true
		if hasPodUsage {
			if nu, ok := nodeUsage[p.Spec.NodeName]; ok {
				var others uint64
				if nu.MemoryBytes >= podUsage.MemoryBytes {
					others = nu.MemoryBytes - podUsage.MemoryBytes
				}
				if node.MemoryBytes >= others {
					out.MemoryBytes = node.MemoryBytes - others
				} else {
					out.MemoryBytes = 0
				}
				out.MemAvailable = true
			}
		}
	}
	return out
}
