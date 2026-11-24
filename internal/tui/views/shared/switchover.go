package shared

import "pgmon/internal/monitor"

// SwitchoverActionLines is the compact status shown on Overview and Pods.
func SwitchoverActionLines(ctx Context, width int) []string {
	primary := ctx.Data.Cluster.Status.CurrentPrimary
	target := ""
	nodes := ctx.Nodes()
	if len(nodes) > 0 {
		target = nodes[SelectedIndex(nodes, ctx.SelectedPod)].Name
	}
	lines := []string{}
	if target != "" {
		role := "replica"
		if target == primary {
			role = "primary"
		}
		lines = append(lines, "Selected  "+Bright.Render(target)+Muted.Render(" · "+role))
	} else {
		lines = append(lines, Muted.Render("No instance selected"))
	}
	if primary != "" && primary != target {
		lines = append(lines, Muted.Render("Current primary  "+primary))
	}
	controls := "↑↓ select · p switchover"
	if width < 70 {
		controls = "↑↓ select · p switchover"
	}
	lines = append(lines, Muted.Render(controls))
	if ok, _ := monitor.CanSwitchover(ctx.Data, target); ok {
		lines = append(lines, Muted.Render("Promote "+target+" via kubectl cnpg promote"))
	} else {
		_, reason := monitor.CanSwitchover(ctx.Data, target)
		lines = append(lines, Muted.Render("Switchover unavailable: "+ValueOr(reason, "not ready")))
	}
	if ctx.SwitchoverBusy {
		lines = append(lines, Amber.Render("working · "+ValueOr(ctx.SwitchoverNotice, "request in progress")))
	} else if ctx.SwitchoverNotice != "" {
		lines = append(lines, ctx.SwitchoverNotice)
	}
	if monitor.SwitchoverInProgress(ctx.Data.Cluster) && ctx.SwitchoverPending == "" {
		lines = append(lines, Amber.Render("Switchover in progress: "+ctx.Data.Cluster.Status.CurrentPrimary+" → "+ctx.Data.Cluster.Status.TargetPrimary))
	}
	return lines
}

// SwitchoverConfirmPanel is the y/Enter prompt for a planned promote.
func SwitchoverConfirmPanel(ctx Context) (string, []string) {
	controls := Muted.Render("y/Enter confirm · Esc cancel")
	lines := []string{
		"Promote replica " + Bright.Render(ctx.SwitchoverConfirmTarget) + " to primary",
		"Current primary  " + ValueOr(ctx.SwitchoverConfirmPrimary, "—"),
		"Cluster          " + ValueOr(ctx.Cluster, "—") + Muted.Render("  in  "+ValueOr(ctx.Namespace, "—")),
		Muted.Render("Graceful switchover via kubectl cnpg promote. Writes pause briefly."),
		controls,
	}
	return "Confirm switchover", lines
}
