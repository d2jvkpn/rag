.PHONY: backend_check backend_build backend_run frontend_run frontend_build build_image

git_branch := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)
git_commit := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
commit_time := $(shell git log -1 --format=%cI 2>/dev/null || echo unknown)
version_pkg := github.com/d2jvkpn/rag/backend/internal/infra

backend_check:
	$(MAKE) -C backend check

backend_build:
	$(MAKE) -C backend build

backend_run:
	$(MAKE) -C backend run

frontend_run:
	$(MAKE) -C frontend run

frontend_build:
	$(MAKE) -C frontend build

build_image:
	docker build -f deploy/Containerfile \
	  --no-cache \
	  --build-arg build_region=cn \
	  --build-arg PUID=1000 \
	  --build-arg PGID=1000 \
	  --build-arg git_branch="$(git_branch)" \
	  --build-arg git_commit="$(git_commit)" \
	  --build-arg commit_time="$(commit_time)" \
	  --build-arg version_pkg="$(version_pkg)" \
	  -t q.local/rag:latest ./
