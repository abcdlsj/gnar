all:
	go build -ldflags "-X github.com/abcdlsj/pipe/share.BuildStamp=`date +'%Y-%m-%d_%H:%M.%S'` -X github.com/abcdlsj/pipe/share.GitHash=`git rev-parse --short HEAD`"

clean:
	rm -f ./pipe

prebuild_startpy:
	make all
	killall pipe || true
	killall Python || true
	python3 -m http.server 3000 &
	
test_simple: prebuild_startpy
	./pipe server -t 'make test' 2>&1 /dev/null &
	sleep 1

	./pipe client -s 127.0.0.1:8910 -l 3000 -u 9100 -t 'make test' 2>&1 /dev/null &
	sleep 1

	curl -I http://127.0.0.1:9100

	sleep 5

	killall pipe || true
	killall Python || true

test_simple_udp: prebuild_startpy
	./pipe server -t 'make test' 2>&1 /dev/null &
	sleep 1

	./pipe client -s 127.0.0.1:8910 -l 3000 -u 9100 -t 'make test' -y 'udp' 2>&1 /dev/null &
	sleep 1

	nc -u localhost 9100

	sleep 5

	killall pipe || true
	killall Python || true


test_auth: prebuild_startpy
	./pipe server -t 'make test-auth' 2>&1 /dev/null &
	sleep 1

	./pipe client -s 127.0.0.1:8910 -l 3000 -u 9100 -t 'make test-auth failed' 2>&1 /dev/null &
	sleep 1

	# curl -I http://127.0.0.1:9100

	sleep 5

	killall pipe || true
	killall Python || true