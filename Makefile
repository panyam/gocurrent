.PHONY: test test-race test-verbose lint

# Run tests
test:
	go test ./...

# Run tests with race detector (primary verification target)
test-race:
	go test -race -count=5 -timeout 150s ./...

# Run tests with verbose output and race detector
test-verbose:
	go test -race -count=5 -timeout 150s -v ./...

# Run go vet
lint:
	go vet ./...
