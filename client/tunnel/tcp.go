package tunnel

import (
	"fmt"
	"io"
	"net"

	"github.com/abcdlsj/gnar/logger"
	"github.com/abcdlsj/gnar/proxy"
)

type TCP struct {
	lport  int
	rconn  io.ReadWriteCloser
	logger *logger.Logger
}

func NewTCP(lport int, rconn io.ReadWriteCloser, tlogger *logger.Logger) *TCP {
	return &TCP{
		lport:  lport,
		rconn:  rconn,
		logger: tlogger,
	}
}

func (t *TCP) Run() {
	lConn, err := net.Dial("tcp", fmt.Sprintf(":%d", t.lport))
	if err != nil {
		t.logger.Errorf("Error connecting to local: %v, will close proxy", err)
		return
	}

	proxy.Stream(t.rconn, lConn)
}
