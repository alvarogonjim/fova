.PHONY: build test run lint install

build:
	go build -ldflags='-s -w' -o bin/fova ./cmd/fova

run: build
	./bin/fova

test:
	go test ./...

lint:
	go vet ./...

install: build
	install -m 0755 bin/fova /usr/local/bin/fova
