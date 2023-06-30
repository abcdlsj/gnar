all:
	go build -ldflags "-X main.buildStamp=`date +'%Y-%m-%d_%H:%M.%S'` -X main.gitHash=`git rev-parse --short HEAD`"

clean:
	rm -f ./gpipe