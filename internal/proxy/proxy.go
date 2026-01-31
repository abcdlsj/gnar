package proxy

import (
	"io"
	"net"
	"strings"
	"sync"

	"github.com/abcdlsj/gnar/internal/logger"
	"github.com/abcdlsj/gnar/pkg/proto"
)

var bufPool = sync.Pool{
	New: func() any {
		return make([]byte, 32*1024)
	},
}

func Stream(s1, s2 io.ReadWriteCloser) {
	s1 = rwcWrap(s1)
	s2 = rwcWrap(s2)

	defer s1.Close()
	defer s2.Close()

	copy := func(dst io.Writer, src io.Reader) {
		buf := bufPool.Get().([]byte)
		defer bufPool.Put(buf)
		io.CopyBuffer(dst, src, buf)
	}

	go copy(s2, s1)
	copy(s1, s2)
}

func rwcWrap(rwc io.ReadWriteCloser) io.ReadWriteCloser {
	return struct{ io.ReadWriteCloser }{rwc}
}

func UDPClientDatagram(tcp, udp io.ReadWriteCloser) error {
	go func() {
		for {
			msg := proto.MsgUDPDatagram{}
			if err := proto.Recv(tcp, &msg); err != nil {
				logger.Warnf("UDP datagram recv failed: %v", err)
				return
			}
			logger.Debugf("UDP datagram recv [%s]", strings.TrimSpace(string(msg.Payload)))
			if _, err := udp.Write(msg.Payload); err != nil {
				logger.Warnf("UDP write failed: %v", err)
				return
			}
		}
	}()

	buf := make([]byte, 4096)
	for {
		n, err := udp.Read(buf)
		if err != nil {
			logger.Warnf("UDP read failed: %v", err)
			return err
		}
		logger.Debugf("UDP read %d bytes [%s]", n, strings.TrimSpace(string(buf[:n])))
		if err = proto.Send(tcp, proto.NewMsgUDPDatagram(nil, buf[:n])); err != nil {
			logger.Warnf("UDP datagram send failed: %v", err)
			return err
		}
	}
}

func UDPDatagram(tcp io.ReadWriteCloser, udp *net.UDPConn) error {
	buf := make([]byte, 4096)
	for {
		n, addr, err := udp.ReadFromUDP(buf)
		if err != nil {
			logger.Warnf("UDP read failed: %v", err)
			return err
		}
		logger.Debugf("UDP read %d bytes from %v [%s]", n, addr, strings.TrimSpace(string(buf[:n])))
		if err = proto.Send(tcp, proto.NewMsgUDPDatagram(addr, buf[:n])); err != nil {
			logger.Warnf("UDP datagram send failed: %v", err)
			return err
		}

		go func() {
			msg := proto.MsgUDPDatagram{}
			if err := proto.Recv(tcp, &msg); err != nil {
				logger.Warnf("UDP datagram recv failed: %v", err)
				return
			}
			logger.Debugf("UDP datagram recv [%s]", strings.TrimSpace(string(msg.Payload)))
			if _, err := udp.WriteTo(msg.Payload, addr); err != nil {
				logger.Warnf("UDP write failed: %v", err)
			}
		}()
	}
}
