.PHONY: build test run clean build-sshushd
.BINARY=sshush
.BINARYD=sshushd

build: build-sshushd
	go build -o $(.BINARY) .

build-sshushd:
	go build -o $(.BINARYD) ./cmd/sshushd

test:
	go test ./... -v

run:
	go run .

clean:
	rm -f $(.BINARY) $(.BINARYD)

kill:
	-pkill -f $(.BINARY)
	-pkill -f $(.BINARYD)
	-ps -w | grep $(.BINARY) | grep -v grep