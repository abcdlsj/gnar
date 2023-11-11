package tunnel

import (
	"net"

	"github.com/abcdlsj/pipe/logger"
)

func RunTunnel(lport int, proxyType, speedLimit string, tlogger *logger.Logger, rconn net.Conn) {
	switch proxyType {
	case "udp":
		go NewUDP(lport, rconn, tlogger).Run()
	case "tcp":
		go NewTCP(lport, speedLimit, rconn, tlogger).Run()
	default:
		tlogger.Errorf("Unknown proxy type: %s", proxyType)
	}
}
