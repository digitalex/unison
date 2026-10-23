.PHONY: build test web serve

build:
	go build -o unison ./cmd/unison

test:
	go test ./...

# Rebuild the web UI into internal/webui/dist (embedded in the binary).
web:
	cd web && npm install && npm run build

serve:
	go run ./cmd/unison serve
