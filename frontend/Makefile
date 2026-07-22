.PHONY: run build

run:
	CONFIG=app.local.json npm run dev -- --host=0.0.0.0 --port=3062

build:
	BASE_PATH="/ui" OUT_DIR="../backend/target/spa/ui" \
	npm run build
