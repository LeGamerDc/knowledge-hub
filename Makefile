.PHONY: generate build test clean vet

generate:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen --config=api/server.cfg.yaml api/openapi.yaml
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen --config=api/client.cfg.yaml api/openapi.yaml

build: generate
	go build ./...

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -f internal/server/handlers/api.gen.go
	rm -f pkg/khclient/client.gen.go
