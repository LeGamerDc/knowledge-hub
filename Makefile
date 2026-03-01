.PHONY: generate build bin test clean vet seed

generate:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen --config=api/server.cfg.yaml api/openapi.yaml
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen --config=api/client.cfg.yaml api/openapi.yaml

build: generate
	go build ./...

bin:
	mkdir -p bin
	go build -o bin/kh-server ./cmd/kh-server
	go build -o bin/mcp-shim  ./cmd/mcp-shim
	go build -o bin/kh        ./cmd/kh

test:
	go test ./...

vet:
	go vet ./...

seed:
	@./scripts/seed-knowledge.sh

clean:
	rm -f internal/server/handlers/api.gen.go
	rm -f pkg/khclient/client.gen.go
	rm -rf bin/
