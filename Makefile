.PHONY: dev

# Restart the TUI when Go files change. Pass app flags with ARGS.
#   make dev
#   make dev ARGS="-namespace my-namespace -cluster my-cluster"
dev:
	gow run . $(ARGS)
