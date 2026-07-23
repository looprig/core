.PHONY: test fmt fmt-check vet check lint vuln secure fuzz

# Module's own package dirs, excluding vendor/ and the nested .worktrees/ modules
# (go list ./... stops at nested module boundaries and skips vendor).
GO_DIRS := $(shell go list -f '{{.Dir}}' ./...)

test:
	go test -race ./...

# Format the whole module in place.
fmt:
	gofmt -w $(GO_DIRS)

fmt-check:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)

vet:
	go vet ./...

check: fmt-check vet test

lint: fmt-check
	go vet ./...
	go tool staticcheck ./...
	# gosec is NOT module-aware: its ./... is a filesystem walk that descends into
	# the nested .worktrees/ checkouts (separate modules) and, under -mod=vendor,
	# reports modules.txt desyncs for those foreign trees. Scope it to THIS module's
	# package dirs via GO_DIRS (the same go-list idiom fmt/fmt-check use). go vet and
	# staticcheck are module-aware (go list stops at module boundaries), so they need
	# no scoping.
	go tool gosec $(GO_DIRS)

vuln:
	go mod verify
	go tool govulncheck ./...

secure: lint vuln

fuzz:
	@echo "Usage: go test -fuzz=FuzzXxx ./path/to/pkg -fuzztime=30s"
