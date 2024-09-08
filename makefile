.DEFAULT_GOAL := help

help:
	@echo "Usage: make [target]"

build:
	go build -ldflags "-X github.com/abcdlsj/gnar/share.BuildStamp=`date +'%Y-%m-%d_%H:%M.%S'` -X github.com/abcdlsj/gnar/share.GitHash=`git rev-parse --short HEAD`"

install: build
	rm ${GOPATH}/bin/gnar 2> /dev/null || true
	mv gnar ${GOPATH}/bin/

clean:
	rm -f ./gnar

test:
	go test -v ./tests/integration/...

test_server:
	go test -v ./tests/integration/server_test.go

test_client:
	go test -v ./tests/integration/client_test.go

test_mux:
	go test -v ./tests/integration/mux_test.go

.PHONY: help build install clean test
