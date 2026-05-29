.PHONY: build_ui run

run:
	npm run dev -- --host=0.0.0.0 --port=3062

build:
	BASE_PATH="/ui" npm run build
	rm -rf ../backend/target/ui
	cp -r target/dist ../backend/target/ui
