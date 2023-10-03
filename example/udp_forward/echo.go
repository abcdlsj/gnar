package main

import (
	"fmt"
	"log"
	"net"
	"time"
)

func main() {
	udpServer, err := net.ListenPacket("udp", ":3000")
	if err != nil {
		log.Fatal(err)
	}
	defer udpServer.Close()

	for {
		buf := make([]byte, 1024)
		_, addr, err := udpServer.ReadFrom(buf)
		if err != nil {
			continue
		}
		go response(udpServer, addr, buf)
	}

}

func response(udpServer net.PacketConn, addr net.Addr, buf []byte) {
	responseStr := fmt.Sprintf("time: %v, message: %s", time.Now().Format(time.ANSIC), string(buf))
	udpServer.WriteTo([]byte(responseStr), addr)
}
