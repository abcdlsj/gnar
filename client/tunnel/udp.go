package tunnel

import (
	"io"
	"net"

	"github.com/abcdlsj/pipe/logger"
	"github.com/abcdlsj/pipe/proxy"
)

type UDP struct {
	lport  int
	rconn  io.ReadWriteCloser
	logger *logger.Logger
}

func NewUDP(lport int, rconn io.ReadWriteCloser, tlogger *logger.Logger) *UDP {
	return &UDP{
		lport:  lport,
		rconn:  rconn,
		logger: tlogger,
	}
}

func (u *UDP) Run() {
	lConn, err := net.DialUDP("udp", nil, &net.UDPAddr{
		IP:   net.ParseIP("0.0.0.0"),
		Port: u.lport,
	})

	if err != nil {
		u.logger.Errorf("Error connecting to local: %v, will close proxy, %s:%d", err, u.lport)
		return
	}

	if err := proxy.UDPClientDatagram(u.rconn, lConn); err != nil {
		u.logger.Errorf("Error proxying udp: %v", err)
		return
	}
}
