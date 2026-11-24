package monitor

import (
	"encoding/json"
	"os"
	"testing"
)

func TestParseCPUQuantity(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"100m", 0.1},
		{"2", 2},
		{"500n", 0.0000005},
		{"250u", 0.00025},
	}
	for _, tc := range cases {
		got, ok := ParseCPUQuantity(tc.in)
		if !ok || abs64(got-tc.want) > 1e-12 {
			t.Errorf("ParseCPUQuantity(%q) = %v, %v", tc.in, got, ok)
		}
	}
	if _, ok := ParseCPUQuantity(""); ok {
		t.Fatal("empty CPU should be rejected")
	}
}

func TestParseMemoryQuantity(t *testing.T) {
	cases := []struct {
		in   string
		want uint64
	}{
		{"256Mi", 256 << 20},
		{"1298332Ki", 1298332 * 1024},
		{"1Gi", 1 << 30},
		{"1024", 1024},
		{"2k", 2000},
	}
	for _, tc := range cases {
		got, ok := ParseMemoryQuantity(tc.in)
		if !ok || got != tc.want {
			t.Errorf("ParseMemoryQuantity(%q) = %d, %v; want %d", tc.in, got, ok, tc.want)
		}
	}
}

func TestUsageByNameSumsPodContainers(t *testing.T) {
	list := metricsList{Items: []metricsItem{{
		Containers: []struct {
			Usage resourceQuantity `json:"usage"`
		}{
			{Usage: resourceQuantity{CPU: "50m", Memory: "100Mi"}},
			{Usage: resourceQuantity{CPU: "25m", Memory: "28Mi"}},
		},
	}}}
	list.Items[0].Metadata.Name = "postgres-1"
	got := usageByName(list, true)["postgres-1"]
	if abs64(got.CPUCores-0.075) > 1e-9 || got.MemoryBytes != 128<<20 {
		t.Fatalf("pod sum = %+v", got)
	}
}

func TestPodResourceCapPrefersLimits(t *testing.T) {
	var p PodResource
	if err := json.Unmarshal([]byte(`{"spec":{"containers":[{"resources":{"limits":{"cpu":"1","memory":"1Gi"},"requests":{"cpu":"100m","memory":"256Mi"}}}]}}`), &p); err != nil {
		t.Fatal(err)
	}
	got := PodResourceCap(&p)
	if !got.HasCPU || !got.HasMemory || got.CPUCores != 1 || got.MemoryBytes != 1<<30 {
		t.Fatalf("cap = %+v", got)
	}
}

func TestNodeResourceCapPrefersAllocatable(t *testing.T) {
	var n NodeResource
	if err := json.Unmarshal([]byte(`{"metadata":{"name":"worker-1"},"status":{"allocatable":{"cpu":"7820m","memory":"16Gi"},"capacity":{"cpu":"8","memory":"32Gi"}}}`), &n); err != nil {
		t.Fatal(err)
	}
	got := NodeResourceCap(n)
	if !got.HasCPU || !got.HasMemory || abs64(got.CPUCores-7.82) > 1e-9 || got.MemoryBytes != 16<<30 {
		t.Fatalf("cap = %+v", got)
	}
}

func TestResolveResourceCapFallsBackToNode(t *testing.T) {
	var p PodResource
	if err := json.Unmarshal([]byte(`{"spec":{"nodeName":"worker-1","containers":[{"name":"postgres"}]}}`), &p); err != nil {
		t.Fatal(err)
	}
	got := ResolveResourceCap(&p, map[string]ResourceCap{"worker-1": {CPUCores: 8, MemoryBytes: 32 << 30, HasCPU: true, HasMemory: true}})
	if !got.HasCPU || !got.HasMemory || got.CPUCores != 8 || got.MemoryBytes != 32<<30 {
		t.Fatalf("cap = %+v", got)
	}
	limited := ResolveResourceCap(&PodResource{}, map[string]ResourceCap{"worker-1": {CPUCores: 8, HasCPU: true}})
	if limited.HasCPU {
		t.Fatal("empty pod should not pick a node cap")
	}
}

func TestResolveEffectiveCapSubtractsOtherNodeWorkloads(t *testing.T) {
	var p PodResource
	if err := json.Unmarshal([]byte(`{"spec":{"nodeName":"worker-1","containers":[{"name":"postgres"}]}}`), &p); err != nil {
		t.Fatal(err)
	}
	nodeCaps := map[string]ResourceCap{"worker-1": {CPUCores: 8, MemoryBytes: 32 << 30, HasCPU: true, HasMemory: true}}
	nodeUsage := map[string]ResourceUsage{"worker-1": {CPUCores: 2, MemoryBytes: 20 << 30}}
	podUsage := ResourceUsage{CPUCores: 0.5, MemoryBytes: 16 << 30}
	got := ResolveEffectiveCap(&p, nodeCaps, nodeUsage, podUsage, true)
	// Available = allocatable - (node - pod): CPU 8-(2-0.5)=6.5, MEM 32-(20-16)=28.
	if !got.CPUFromNode || !got.CPUAvailable || abs64(got.CPUCores-6.5) > 1e-9 || !got.HasNodeCPU || got.NodeCPUCores != 8 {
		t.Fatalf("CPU cap = %+v", got)
	}
	if !got.MemFromNode || !got.MemAvailable || got.MemoryBytes != 28<<30 || !got.HasNodeMemory || got.NodeMemoryBytes != 32<<30 {
		t.Fatalf("MEM cap = %+v", got)
	}
}

func TestResolveEffectiveCapFallsBackToNodeTotalWithoutNodeUsage(t *testing.T) {
	var p PodResource
	if err := json.Unmarshal([]byte(`{"spec":{"nodeName":"worker-1","containers":[{"name":"postgres"}]}}`), &p); err != nil {
		t.Fatal(err)
	}
	nodeCaps := map[string]ResourceCap{"worker-1": {CPUCores: 8, MemoryBytes: 32 << 30, HasCPU: true, HasMemory: true}}
	got := ResolveEffectiveCap(&p, nodeCaps, nil, ResourceUsage{CPUCores: 0.5}, true)
	if !got.CPUFromNode || got.CPUAvailable || got.CPUCores != 8 {
		t.Fatalf("without node usage should use node total: %+v", got)
	}
}

func TestResolveEffectiveCapPrefersPodLimits(t *testing.T) {
	var p PodResource
	if err := json.Unmarshal([]byte(`{"spec":{"nodeName":"worker-1","containers":[{"resources":{"limits":{"cpu":"1","memory":"1Gi"}}}]}}`), &p); err != nil {
		t.Fatal(err)
	}
	nodeCaps := map[string]ResourceCap{"worker-1": {CPUCores: 8, MemoryBytes: 32 << 30, HasCPU: true, HasMemory: true}}
	nodeUsage := map[string]ResourceUsage{"worker-1": {CPUCores: 2, MemoryBytes: 20 << 30}}
	got := ResolveEffectiveCap(&p, nodeCaps, nodeUsage, ResourceUsage{CPUCores: 0.5, MemoryBytes: 512 << 20}, true)
	if got.CPUFromNode || got.CPUCores != 1 || got.MemoryBytes != 1<<30 {
		t.Fatalf("pod limits should win: %+v", got)
	}
}

func TestFormatCPUAndMem(t *testing.T) {
	if FormatCPU(1) != "1" || FormatCPU(0.045) != "45m" {
		t.Fatalf("FormatCPU: %q %q", FormatCPU(1), FormatCPU(0.045))
	}
	if FormatMem(256<<20) != "256Mi" || FormatMem(1<<30) != "1Gi" {
		t.Fatalf("FormatMem: %q %q", FormatMem(256<<20), FormatMem(1<<30))
	}
}

func TestAppendUsageHistoriesKeepsRecentSamples(t *testing.T) {
	prev := map[string]UsageHistory{"postgres-1": {CPU: []float64{0.01, 0.02}, Memory: []float64{10, 20}}}
	got := appendUsageHistories(prev, map[string]ResourceUsage{"postgres-1": {CPUCores: 0.03, MemoryBytes: 30}}, 3)
	if len(got["postgres-1"].CPU) != 3 || got["postgres-1"].CPU[2] != 0.03 || got["postgres-1"].Memory[2] != 30 {
		t.Fatalf("history = %+v", got["postgres-1"])
	}
}

func TestCollectUsageReadsMetricsAPI(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"if printf '%s' \"$*\" | grep -q 'namespaces/.*/pods'; then\n" +
		"  printf '%s\\n' '{\"items\":[{\"metadata\":{\"name\":\"postgres-1\"},\"containers\":[{\"usage\":{\"cpu\":\"50m\",\"memory\":\"128Mi\"}}]}]}'\n" +
		"  exit 0\n" +
		"fi\n" +
		"if printf '%s' \"$*\" | grep -q '/nodes'; then\n" +
		"  printf '%s\\n' '{\"items\":[{\"metadata\":{\"name\":\"worker-1\"},\"usage\":{\"cpu\":\"2\",\"memory\":\"8Gi\"}}]}'\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 1\n"
	if err := os.WriteFile(dir+"/kubectl", []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var s Snapshot
	collectUsage(Config{Context: "ctx", Namespace: "db"}, &s)
	if s.MetricsErr != "" {
		t.Fatal(s.MetricsErr)
	}
	pod := s.PodUsage["postgres-1"]
	node := s.NodeUsage["worker-1"]
	if abs64(pod.CPUCores-0.05) > 1e-9 || pod.MemoryBytes != 128<<20 {
		t.Fatalf("pod usage = %+v", pod)
	}
	if abs64(node.CPUCores-2) > 1e-9 || node.MemoryBytes != 8<<30 {
		t.Fatalf("node usage = %+v", node)
	}
}

func abs64(n float64) float64 {
	if n < 0 {
		return -n
	}
	return n
}
