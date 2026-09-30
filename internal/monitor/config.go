package monitor

import "time"

const (
	refreshEvery = 5 * time.Second
	kubeEvery    = 15 * time.Second
)

type Config struct{ Context, Namespace, Cluster string }
