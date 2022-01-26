package server

import (
	"fmt"
	"net"

	"github.com/abcdlsj/gpipe/layer"
	"github.com/abcdlsj/gpipe/proxy"
	"storj.io/common/uuid"
)

func run() {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		logger.Fatalf("Error listening: %v", err)
	}
	logger.Infof("Listening on port %d", port)
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			logger.Infof("Error accepting: %v", err)
			return
		}

		go handle(conn)
	}
}

func handle(conn net.Conn) {
	buf := read(conn)
	if buf == nil {
		return
	}
	switch layer.PacketType(buf[0]) {
	case layer.RegisterForward:
		handleRegister(conn, buf)
	case layer.ExchangeMsg:
		handleMessage(conn, buf)
	case layer.CancelForward:
		handleCancel(conn, buf)
	}
}

func handleCancel(conn net.Conn, buf []byte) {
	// TODO
	// close user port listener
	// close user connection
}

func handleRegister(conn net.Conn, buf []byte) {
	uPort := parseRegisterPacket(buf)
	if isInvaliedPort(uPort) {
		logger.WithField("port", uPort).Errorf("Invalid port")
		return
	}
	uListener, err := net.Listen("tcp", fmt.Sprintf(":%d", uPort))
	if err != nil {
		logger.WithField("port", uPort).Errorf("Error listening: %v", err)
		return
	}
	defer uListener.Close()

	logger.WithField("port", uPort).Infof("Create user port listener")

	for {
		uConn, err := uListener.Accept()
		if err != nil {
			logger.Errorf("Error accepting: %v", err)
			continue
		}
		logger.WithField("port", uPort).Debugf("Accept user connection")

		go func() {
			cid := genUuid(5)
			connMap.Add(cid, uConn)
			sendExchangeMsg(conn, cid)
		}()
	}
}

func handleMessage(conn net.Conn, buf []byte) {
	rid := parseExchangePacket(buf)
	uConn, ok := connMap.Get(rid)
	if !ok {
		return
	}

	defer connMap.Del(rid)

	proxy.P(conn, uConn)
}

func sendExchangeMsg(conn net.Conn, id string) error {
	payload := make([]byte, layer.Len)
	payload = append(payload, byte(layer.ExchangeMsg))
	payload = append(payload, []byte(id)...)

	_, err := conn.Write(payload)
	if err != nil {
		logger.Errorf("Error sending data: %v", err)
		return err
	}

	return nil
}

func read(conn net.Conn) []byte {
	buf := make([]byte, layer.Len)
	_, err := conn.Read(buf)
	if err != nil {
		return nil
	}
	return buf
}

func genUuid(n int) string {
	nid, err := uuid.New()
	if err != nil {
		logger.Println(err)
	}
	return nid.String()[:n]
}

func parseRegisterPacket(buf []byte) int {
	return int(buf[1])<<8 + int(buf[2])
}

func parseCancelPacket(buf []byte) int {
	return int(buf[1])<<8 + int(buf[2])
}

func parseExchangePacket(buf []byte) string {
	return string(buf[1:])
}

func isInvaliedPort(port int) bool {
	return port < 0 || port > 65535
}
