package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type DiscoveryResult struct {
	Config   Config
	Clusters []ClusterResource
	Err      error
}

func discoveryKubectl(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("kubectl discovery timed out after 8 seconds")
	}
	if err != nil {
		return nil, fmt.Errorf("kubectl: %s", fallback(strings.TrimSpace(stderr.String()), err.Error()))
	}
	return out, nil
}

// Resolve the context once, then pin every request to it for this session.
func Discover(cfg Config, runner ...func(...string) ([]byte, error)) DiscoveryResult {
	run := discoveryKubectl
	if len(runner) > 0 && runner[0] != nil {
		run = runner[0]
	}
	result := DiscoveryResult{Config: cfg}
	if cfg.Context == "" {
		out, err := run("config", "current-context")
		if err != nil {
			result.Err = fmt.Errorf("read current context: %w", err)
			return result
		}
		cfg.Context = strings.TrimSpace(string(out))
		if cfg.Context == "" {
			result.Err = fmt.Errorf("no current kubectl context is configured")
			return result
		}
		result.Config = cfg
	}
	args := []string{"--context", cfg.Context, "get", "clusters.postgresql.cnpg.io"}
	exact := cfg.Cluster != "" && cfg.Namespace != ""
	if exact {
		args = append(args, cfg.Cluster)
	}
	if cfg.Namespace != "" {
		args = append(args, "--namespace", cfg.Namespace)
	} else {
		args = append(args, "--all-namespaces")
	}
	args = append(args, "-o", "json")
	out, err := run(args...)
	if err != nil {
		result.Err = err
		return result
	}
	var clusters []ClusterResource
	if exact {
		var cluster ClusterResource
		err = json.Unmarshal(out, &cluster)
		clusters = []ClusterResource{cluster}
	} else {
		var list struct {
			Items []ClusterResource `json:"items"`
		}
		err = json.Unmarshal(out, &list)
		clusters = list.Items
	}
	if err != nil {
		result.Err = fmt.Errorf("decode cluster discovery: %w", err)
		return result
	}
	for _, c := range clusters {
		if c.Metadata.Name == "" || c.Metadata.Namespace == "" {
			result.Err = fmt.Errorf("cluster discovery returned an object without a name or namespace")
			return result
		}
		if cfg.Cluster == "" || c.Metadata.Name == cfg.Cluster {
			result.Clusters = append(result.Clusters, c)
		}
	}
	sort.Slice(result.Clusters, func(i, j int) bool {
		a, b := result.Clusters[i].Metadata, result.Clusters[j].Metadata
		if a.Namespace == b.Namespace {
			return a.Name < b.Name
		}
		return a.Namespace < b.Namespace
	})
	return result
}

func fallback(value, alternate string) string {
	if value != "" {
		return value
	}
	return alternate
}
