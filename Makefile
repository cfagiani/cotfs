.PHONY: test clean format deps build install all

all: clean deps build install

build:
	go build

install:
	go install ./...

test:
	go test -cover  ./...

format:
	gofmt -w ./

clean:
	go clean ./...

deps:
	go get bazil.org/fuse
	go get github.com/mattn/go-sqlite3
