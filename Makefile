.PHONY: backend_check backend_run frontend_run frontend_build

backend_check:
	$(MAKE) -C backend check

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
	  --build-arg PU=GID=1000 \
	  -t q.local/rag:latest ./
