package proxy

import (
	"io"
	"net"
	"strings"

	"github.com/abcdlsj/gnar/logger"
	"github.com/abcdlsj/gnar/proto"
)

func UDPClientDatagram(tcp, udp io.ReadWriteCloser) error {
	go func() {
		for {
			msg := proto.MsgUDPDatagram{}
			if err := proto.Recv(tcp, &msg); err != nil {
				logger.Warnf("Msg udp datagram recv failed: %v", err)
				return
			}
			logger.Debugf("Msg udp datagram recv [%s]", strings.TrimSpace(string(msg.Payload)))
			n, err := udp.Write(msg.Payload)
			if err != nil {
				logger.Warnf("UDP write failed: %v", err)
				return
			}

			if n != len(msg.Payload) {
				logger.Warnf("UDP write failed: %d != %d", n, len(msg.Payload))
				return
			}
		}
	}()

	for {
		buf := make([]byte, 4096)
		n, err := udp.Read(buf)
		if err != nil {
			logger.Warnf("UDP read failed: %v", err)
			return err
		}
		logger.Debugf("UDP read %d bytes from %v, [%s]", n, strings.TrimSpace(string(buf[:n])))
		if err = proto.Send(tcp, proto.NewMsgUDPDatagram(nil, buf[:n])); err != nil {
			logger.Warnf("Msg udp datagram send failed: %v", err)
			return err
		}
	}
}

func UDPDatagram(tcp io.ReadWriteCloser, udp *net.UDPConn) error {
	for {
		buf := make([]byte, 4096)
		n, addr, err := udp.ReadFromUDP(buf)
		if err != nil {
			logger.Warnf("UDP read failed: %v", err)
			return err
		}
		logger.Debugf("UDP read %d bytes from %v, [%s]", n, addr, strings.TrimSpace(string(buf[:n])))
		if err = proto.Send(tcp, proto.NewMsgUDPDatagram(addr, buf[:n])); err != nil {
			logger.Warnf("Msg udp datagram send failed: %v", err)
			return err
		}

		go func() {
			msg := proto.MsgUDPDatagram{}
			if err := proto.Recv(tcp, &msg); err != nil {
				logger.Warnf("Msg udp datagram recv failed: %v", err)
				return
			}
			logger.Debugf("Msg udp datagram recv [%s]", strings.TrimSpace(string(msg.Payload)))
			_, err := udp.WriteTo(msg.Payload, addr)
			if err != nil {
				logger.Warnf("UDP write failed: %v", err)
				return
			}
		}()
	}
}
