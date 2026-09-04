
# Build the binary
build:
	@echo "Building..."
	go build -o bin/passion-index main.go

# Run locally (reads .env automatically via hopebox/env)
run:
	go run main.go

# Regenerate GraphQL code after editing graph/schema/*.graphql
	@echo "Generating GraphQL code..."

# Run all tests
test:
	go test ./...

# Format code
fmt:
	gofmt -s -w .

# Tidy dependencies
tidy:
	go mod tidy

# Update hopebox dependency
hopebox:
	go get -u github.com/yichozy/hopebox
	go mod tidy

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf bin tmp

# Verify build environment
verify:
	go vet ./...
	go build -o /dev/null main.go
