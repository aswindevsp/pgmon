package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func GetJSON(cfg Config, out any, args ...string) error {
	full := append([]string{"--context", cfg.Context, "-n", cfg.Namespace}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kubectl", full...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("kubectl timed out")
		}
		return fmt.Errorf("kubectl %s: %s", strings.Join(args[:min(2, len(args))], " "), strings.TrimSpace(stderr.String()))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode kubectl JSON: %w", err)
	}
	return nil
}

func KubectlExec(cfg Config, timeout time.Duration, args ...string) ([]byte, error) {
	full := append([]string{"--context", cfg.Context, "-n", cfg.Namespace}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kubectl", full...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("kubectl timed out")
		}
		return nil, fmt.Errorf("kubectl %s: %s", strings.Join(args[:min(2, len(args))], " "), strings.TrimSpace(stderr.String()))
	}
	return bytes.TrimSpace(data), nil
}
