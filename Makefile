.PHONY: build test run lint install

build:
	go build -ldflags='-s -w' -o bin/proteus ./cmd/proteus

run: build
	./bin/proteus

test:
	go test ./...

lint:
	go vet ./...

install: build
	install -m 0755 bin/proteus /usr/local/bin/proteus
