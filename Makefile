.PHONY: build test run clean build-sshushd
.BINARY=sshush
.BINARYD=sshushd
LDFLAGS=-X github.com/ollykeran/sshush/internal/version.Version=dev

build: build-sshushd
	go build -ldflags '$(LDFLAGS)' -o $(.BINARY) .

build-sshushd:
	go build -ldflags '$(LDFLAGS)' -o $(.BINARYD) ./cmd/sshushd

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