package monitor

func AppendHistory(history []float64, value float64, limit int) []float64 {
	start := 0
	if len(history) >= limit {
		start = len(history) - limit + 1
	}
	next := make([]float64, 0, len(history)-start+1)
	next = append(next, history[start:]...)
	return append(next, value)
}

func UpdatedLagHistory(previous map[string][]float64, replicas []ReplicationStatus) map[string][]float64 {
	next := make(map[string][]float64, len(replicas))
	for _, replica := range replicas {
		if replica.LagBytes == nil || replica.ApplicationName == "" {
			continue
		}
		next[replica.ApplicationName] = AppendHistory(previous[replica.ApplicationName], float64(*replica.LagBytes), 60)
	}
	return next
}
