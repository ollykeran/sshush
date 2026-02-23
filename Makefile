.PHONY: build test run clean
.BINARY=sshush

build:
	go build -o $(.BINARY) main.go

test: 
	go test ./...

run:
	go run main.go

clean: 
	rm -f $(.BINARY)