all:
	go build -ldflags "-X github.com/abcdlsj/pipe/share.BuildStamp=`date +'%Y-%m-%d_%H:%M.%S'` -X github.com/abcdlsj/pipe/share.GitHash=`git rev-parse --short HEAD`"

clean:
	rm -f ./pipe

prebuild_startpy:
	make all
	killall pipe || true
	killall Python || true
	killall python3 || true
	python3 -m http.server 10010 &

test_kill:
	killall pipe || true
	killall Python || true
	killall python3 || true

test_simple: prebuild_startpy
	./pipe server -t 'make test' 2>&1 /dev/null &
	sleep 1

	./pipe client -s 127.0.0.1:8910 -l 10010 -u 10020 -t 'make test' 2>&1 /dev/null &
	sleep 1

	curl -I http://127.0.0.1:10020

	sleep 5

	killall pipe || true
	killall Python || true

test_simple_mux: prebuild_startpy
	DEBUG=true ./pipe server -t 'make test' -m 2>&1 /dev/null &
	sleep 1

	DEBUG=true ./pipe client -s 127.0.0.1:8910 -l 10010 -u 10020 -t 'make test' -m 2>&1 /dev/null &
	sleep 1

	curl -I http://127.0.0.1:10020

	sleep 5

	killall pipe || true
	killall Python || true

test_simple_udp: prebuild_startpy
	./pipe server -t 'make test' 2>&1 /dev/null &
	sleep 1

	./pipe client -s 127.0.0.1:8910 -l 10010 -u 10020 -t 'make test' -y 'udp' 2>&1 /dev/null &
	sleep 1

	nc -u localhost 10020

	sleep 5

	killall pipe || true
	killall Python || true


test_auth: prebuild_startpy
	./pipe server -t 'make test-auth' 2>&1 /dev/null &
	sleep 1

	./pipe client -s 127.0.0.1:8910 -l 10010 -u 10020 -t 'make test-auth failed' 2>&1 /dev/null &
	sleep 1

	# curl -I http://127.0.0.1:10020

	sleep 5

	killall pipe || true
	killall Python || true
