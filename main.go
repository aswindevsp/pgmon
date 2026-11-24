package main

import (
	"flag"
	"fmt"
	"os"

	"pgmon/internal/monitor"
	"pgmon/internal/tui"
)

func main() {
	cfg := monitor.Config{}
	flag.StringVar(&cfg.Context, "context", "", "kubectl context (default: current context)")
	flag.StringVar(&cfg.Namespace, "namespace", "", "limit cluster discovery to this namespace (default: all namespaces)")
	flag.StringVar(&cfg.Cluster, "cluster", "", "CloudNativePG cluster name (default: discover and select)")
	flag.Parse()
	if err := tui.Run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "pgmon:", err)
		os.Exit(1)
	}
}
