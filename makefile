.DEFAULT_GOAL := help

help:
	@echo "Usage: make [target]"

build:
	go build -o gnar -ldflags "-X github.com/abcdlsj/gnar/pkg/share.BuildStamp=`date +'%Y-%m-%d_%H:%M.%S'` -X github.com/abcdlsj/gnar/pkg/share.GitHash=`git rev-parse --short HEAD`" ./cmd/gnar

install: build
	rm ${GOPATH}/bin/gnar 2> /dev/null || true
	mv gnar ${GOPATH}/bin/

clean:
	rm -f ./gnar

test:
	go test -v ./test/integration/...

test_server:
	go test -v ./test/integration/server_test.go

test_client:
	go test -v ./test/integration/client_test.go

test_mux:
	go test -v ./test/integration/mux_test.go

.PHONY: help build install clean test