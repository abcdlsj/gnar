package tunnel

import (
	"fmt"
	"net"

	"github.com/abcdlsj/pipe/logger"
	"github.com/abcdlsj/pipe/pio"
	"github.com/abcdlsj/pipe/proxy"
)

type TCP struct {
	lport  int
	rconn  net.Conn
	slimit string
	logger *logger.Logger
}

func NewTCP(lport int, slimit string, rconn net.Conn, tlogger *logger.Logger) *TCP {
	return &TCP{
		lport:  lport,
		rconn:  rconn,
		slimit: slimit,
		logger: tlogger,
	}
}

func (t *TCP) Run() {
	lconn, err := net.Dial("tcp", fmt.Sprintf(":%d", t.lport))
	if err != nil {
		t.logger.Errorf("Error connecting to local: %v, will close proxy", err)
		return
	}

	if t.slimit != "" {
		limit := pio.LimitTransfer(t.slimit)
		t.logger.Debugf("Proxying with limit: %s, transfered limit: %d", t.slimit, limit)
		proxy.Stream(pio.NewLimitStream(lconn, limit), t.rconn)
	}

	proxy.Stream(lconn, t.rconn)
}
