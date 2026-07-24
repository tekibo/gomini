.PHONY: build run clean docker-build docker-run test

BINARY=gomini

build:
	go build -o $(BINARY) ./cmd/gomini

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)

docker-build:
	docker build -t gomini .

docker-run:
	docker run --rm -p 4983:8081 -v $(PWD)/config.json:/etc/gomini/config.json gomini

test:
	go test ./...

test-smoke:
	./test.sh

vet:
	go vet ./...
