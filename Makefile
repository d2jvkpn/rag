.PHONY: backend_check backend_build backend_run mcp_check mcp_build mcp_run frontend_run frontend_build build_image

PUID ?= 1000
PGID ?= 1000

git_branch := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)
git_commit := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
commit_time := $(shell git log -1 --format=%cI 2>/dev/null || echo unknown)
version_pkg := github.com/d2jvkpn/rag/backend/pkg/infra

backend_check:
	$(MAKE) -C backend check

backend_build:
	$(MAKE) -C backend build

backend_run:
	$(MAKE) -C frontend build
	$(MAKE) -C backend run

mcp_check:
	$(MAKE) -C mcp check

mcp_build:
	$(MAKE) -C mcp build

mcp_run:
	$(MAKE) -C mcp run

frontend_run:
	$(MAKE) -C frontend run

frontend_build:
	$(MAKE) -C frontend build

build_image:
	docker build -f deploy/Containerfile \
	  --no-cache \
	  --build-arg build_region=cn \
	  --build-arg PUID="$(PUID)" \
	  --build-arg PGID="$(PGID)" \
	  --build-arg git_branch="$(git_branch)" \
	  --build-arg git_commit="$(git_commit)" \
	  --build-arg commit_time="$(commit_time)" \
	  --build-arg version_pkg="$(version_pkg)" \
	  -t q.local/rag:latest ./

archive_image:
	mkdir -p target
	docker save q.local/rag:latest | gzip -c > target/q.local--rag--latest.tgz
