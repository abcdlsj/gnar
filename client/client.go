package client

import (
	"fmt"
	"net"

	"github.com/abcdlsj/gpipe/layer"
	"github.com/abcdlsj/gpipe/proxy"
)

func run() {
	rConn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", rHost, rPort))
	if err != nil {
		logger.Fatalf("Error connecting to remote: %v", err)
	}

	if err := sendRegister(rConn, uPort); err != nil {
		logger.Fatalf("Error writing to remote: %v", err)
	}

	for {
		buf := readExchangeMsg(rConn)
		if buf == nil {
			return
		}

		logger.Debug("Receive req from server, start proxying")

		nRonn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", rHost, rPort))
		if err != nil {
			logger.Fatalf("Error connecting to remote: %v", err)
		}

		lConn, err := net.Dial("tcp", fmt.Sprintf(":%d", lPort))
		if err != nil {
			logger.Fatalf("Error connecting to local: %v", err)
		}

		go func() {
			_, err := nRonn.Write(buf)
			logger.Debug("Write back buf to server")
			if err != nil {
				logger.Errorf("Error writing to remote: %v", err)
			}
			proxy.P(lConn, nRonn)
		}()
	}
}

func sendRegister(conn net.Conn, port int) error {
	payload := make([]byte, layer.Len)
	payload[0] = byte(layer.RegisterForward)
	payload[1] = byte(port >> 8)
	payload[2] = byte(port)
	_, err := conn.Write(payload)
	if err != nil {
		return err
	}
	return nil
}

func sendCancel(conn net.Conn, port int) error {
	payload := make([]byte, layer.Len)
	payload[0] = byte(layer.CancelForward)
	payload[1] = byte(port >> 8)
	payload[2] = byte(port)
	_, err := conn.Write(payload)
	if err != nil {
		return err
	}
	return nil
}

func readExchangeMsg(conn net.Conn) []byte {
	buf := make([]byte, layer.Len)
	n, err := conn.Read(buf)
	if err != nil {
		logger.Fatalf("Error reading from remote: %v", err)
	}
	if n != layer.Len {
		logger.Fatalf("Error reading from remote: %v", err)
	}
	return buf
}
