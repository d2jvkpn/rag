.PHONY: build check run

check:
	go fmt ./...
	go test ./...

run:
	go run ./cmd/server  --config=configs/local.yaml --addr=:3061

build:
	go build -o target/server ./cmd/server
