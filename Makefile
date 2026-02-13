.PHONY: bahn test lint

bahn:
	go build -o bahn ./cmd/bahn

test:
	go test ./...

lint:
	go vet ./...
