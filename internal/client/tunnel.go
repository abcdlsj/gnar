package client

import (
	"fmt"
	"io"
	"net"

	"github.com/abcdlsj/gnar/internal/logger"
	"github.com/abcdlsj/gnar/internal/pio"
	"github.com/abcdlsj/gnar/internal/proxy"
)

func runTunnel(lport int, proxyType, speedLimit string, rconn net.Conn) {
	var rwc io.ReadWriteCloser = rconn
	if speedLimit != "" {
		limit := pio.LimitTransfer(speedLimit)
		logger.Debugf("Proxying with limit: %s, transfered limit: %d", speedLimit, limit)
		rwc = pio.NewLimitReadWriter(rwc, limit)
	}

	switch proxyType {
	case "udp":
		runUDPTunnel(lport, rwc)
	case "tcp":
		runTCPTunnel(lport, rwc)
	default:
		logger.Errorf("Unknown proxy type: %s", proxyType)
	}
}

func runTCPTunnel(lport int, rconn io.ReadWriteCloser) {
	lConn, err := net.Dial("tcp", fmt.Sprintf(":%d", lport))
	if err != nil {
		logger.Errorf("Error connecting to local: %v, port: %d", err, lport)
		return
	}
	proxy.Stream(rconn, lConn)
}

func runUDPTunnel(lport int, rconn io.ReadWriteCloser) {
	lConn, err := net.DialUDP("udp", nil, &net.UDPAddr{
		IP:   net.ParseIP("0.0.0.0"),
		Port: lport,
	})
	if err != nil {
		logger.Errorf("Error connecting to local: %v, port: %d", err, lport)
		return
	}
	if err := proxy.UDPClientDatagram(rconn, lConn); err != nil {
		logger.Errorf("Error proxying udp: %v", err)
	}
}
