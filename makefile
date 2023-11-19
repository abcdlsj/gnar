.DEFAULT_GOAL := help

help:
	@echo "Usage: make [target]"

build:
	go build -ldflags "-X github.com/abcdlsj/gnar/share.BuildStamp=`date +'%Y-%m-%d_%H:%M.%S'` -X github.com/abcdlsj/gnar/share.GitHash=`git rev-parse --short HEAD`"

install: build
	rm ${GOPATH}/bin/gnar 2> /dev/null || true
	mv gnar ${GOPATH}bin/

clean:
	rm -f ./gnar

prebuild_startpy:
	make build
	killall gnar || true
	killall Python || true
	killall python3 || true
	python3 -m http.server 10010 &

test_kill:
	killall gnar || true
	killall Python || true
	killall python3 || true

test_simple: prebuild_startpy
	./gnar server -t 'make test' 2>&1 /dev/null &
	sleep 1

	./gnar client -s 127.0.0.1:8910 -l 10010 -u 10020 -t 'make test' 2>&1 /dev/null &
	sleep 1

	curl -I http://127.0.0.1:10020

	sleep 5

	killall gnar || true
	killall Python || true

test_simple_mux: prebuild_startpy
	DEBUG=true ./gnar server -t 'make test' -m 2>&1 /dev/null &
	sleep 1

	DEBUG=true ./gnar client -s 127.0.0.1:8910 -l 10010 -u 10020 -t 'make test' -m 2>&1 /dev/null &
	sleep 1

	curl -I http://127.0.0.1:10020

	sleep 5

	killall gnar || true
	killall Python || true

test_simple_udp: prebuild_startpy
	./gnar server -t 'make test' 2>&1 /dev/null &
	sleep 1

	./gnar client -s 127.0.0.1:8910 -l 10010 -u 10020 -t 'make test' -y 'udp' 2>&1 /dev/null &
	sleep 1

	nc -u localhost 10020

	sleep 5

	killall gnar || true
	killall Python || true


test_auth: prebuild_startpy
	./gnar server -t 'make test-auth' 2>&1 /dev/null &
	sleep 1

	./gnar client -s 127.0.0.1:8910 -l 10010 -u 10020 -t 'make test-auth failed' 2>&1 /dev/null &
	sleep 1

	# curl -I http://127.0.0.1:10020

	sleep 5

	killall gnar || true
	killall Python || true
