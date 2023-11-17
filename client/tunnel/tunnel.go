package tunnel

import (
	"io"
	"net"

	"github.com/abcdlsj/pipe/logger"
	"github.com/abcdlsj/pipe/pio"
)

func RunTunnel(lport int, proxyType, speedLimit string, tlogger *logger.Logger, rconn net.Conn) {
	var rwc io.ReadWriteCloser = rconn
	if speedLimit != "" {
		limit := pio.LimitTransfer(speedLimit)
		tlogger.Debugf("Proxying with limit: %s, transfered limit: %d", speedLimit, limit)
		rwc = pio.NewLimitReadWriter(rwc, limit)
	}

	switch proxyType {
	case "udp":
		go NewUDP(lport, rwc, tlogger).Run()
	case "tcp":
		go NewTCP(lport, rwc, tlogger).Run()
	default:
		tlogger.Errorf("Unknown proxy type: %s", proxyType)
	}
}
