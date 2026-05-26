.PHONY: build check run

build:
	go build -o target/server ./cmd/server

check:
	go build ./...
	go test ./...

run:
	go run ./cmd/server  --config=configs/local.yaml --addr=:3061
