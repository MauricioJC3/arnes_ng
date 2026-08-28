BIN := arnes

# Build the binary once so startup is instant (`go run` recompiles every time).
build:
	go build -o $(BIN) ./cmd/arnes

# Put `arnes` on your PATH (usually ~/go/bin).
install:
	go install ./cmd/arnes

run: build
	./$(BIN)

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -f $(BIN)

.PHONY: build install run test vet clean
