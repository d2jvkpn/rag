.PHONY: build check run

check:
	go fmt ./...
	go test ./...

build:
	go build -o target/mcp .

run:
	go run . --config=configs/mcp.yaml --addr=:3062
