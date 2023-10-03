package proxy

import (
	"io"
	"net"
	"strings"

	"github.com/abcdlsj/pipe/logger"
	"github.com/abcdlsj/pipe/protocol"
)

func ProxyUDPClient(token string, tcp, udp io.ReadWriteCloser) error {
	go func() {
		for {
			msg := protocol.MsgUDPDatagram{}
			if err := msg.Recv(tcp); err != nil {
				logger.WarnF("Msg udp datagram recv failed: %v", err)
				return
			}
			logger.DebugF("Msg udp datagram recv [%s]", strings.TrimSpace(string(msg.Payload)))
			n, err := udp.Write(msg.Payload)
			if err != nil {
				logger.WarnF("UDP write failed: %v", err)
				return
			}

			if n != len(msg.Payload) {
				logger.WarnF("UDP write failed: %d != %d", n, len(msg.Payload))
				return
			}
		}
	}()

	for {
		buf := make([]byte, 4096)
		n, err := udp.Read(buf)
		if err != nil {
			logger.WarnF("UDP read failed: %v", err)
			return err
		}
		logger.DebugF("UDP read %d bytes from %v, [%s]", n, strings.TrimSpace(string(buf[:n])))
		if err = protocol.NewMsgUDPDatagram(token, nil, buf[:n]).Send(tcp); err != nil {
			logger.WarnF("Msg udp datagram send failed: %v", err)
			return err
		}
	}
}
func ProxyUDP(token string, tcp io.ReadWriteCloser, udp *net.UDPConn) error {
	for {
		buf := make([]byte, 4096)
		n, addr, err := udp.ReadFromUDP(buf)
		if err != nil {
			logger.WarnF("UDP read failed: %v", err)
			return err
		}
		logger.DebugF("UDP read %d bytes from %v, [%s]", n, addr, strings.TrimSpace(string(buf[:n])))
		if err = protocol.NewMsgUDPDatagram(token, addr, buf[:n]).Send(tcp); err != nil {
			logger.WarnF("Msg udp datagram send failed: %v", err)
			return err
		}

		go func() {
			msg := protocol.MsgUDPDatagram{}
			if err := msg.Recv(tcp); err != nil {
				logger.WarnF("Msg udp datagram recv failed: %v", err)
				return
			}
			logger.DebugF("Msg udp datagram recv [%s]", strings.TrimSpace(string(msg.Payload)))
			_, err := udp.WriteTo(msg.Payload, addr)
			if err != nil {
				logger.WarnF("UDP write failed: %v", err)
				return
			}
		}()
	}
}
