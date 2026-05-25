.PHONY: backend_check backend_run frontend_run

backend_check:
	$(MAKE) -C backend check

backend_run:
	$(MAKE) -C backend run

frontend_run:
	$(MAKE) -C frontend run
