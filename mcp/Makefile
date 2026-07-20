.PHONY: build check run

git_branch := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)
git_commit := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
commit_time := $(shell git log -1 --format=%cI 2>/dev/null || echo unknown)
version_pkg := github.com/d2jvkpn/rag/backend/pkg/infra
ldflags := -X '$(version_pkg).GitBranch=$(git_branch)' -X '$(version_pkg).GitCommit=$(git_commit)' -X '$(version_pkg).CommitTime=$(commit_time)'

check:
	go fmt ./...
	go test ./...

build:
	go build -ldflags "$(ldflags)" -o target/mcp .

run:
	go run -ldflags "$(ldflags)" . --config=configs/mcp.yaml --addr=:3062
