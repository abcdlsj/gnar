package internal

import (
	"fmt"
	"io"
	"log"
	"net"
)

type Pipe struct {
	Start ConnCfg
	End   ConnCfg
}

type ConnCfg struct {
	Type string
	Host string
}

func (p *Pipe) forward(conn net.Conn) {
	endConn, err := net.Dial(p.End.Type, p.End.Host)
	if err != nil {
		fmt.Println(err)
		return
	}

	go copyConn(endConn, conn)
	go copyConn(conn, endConn)
}

func copyConn(writer, reader net.Conn) {
	defer writer.Close()
	defer reader.Close()

	_, err := io.CopyBuffer(writer, reader, nil)
	if err != nil {
		log.Printf("io.CopyBuffer: %v", err)
	}
}

func (p *Pipe) Handler() {
	log.Printf("start: %s://%s", p.Start.Type, p.Start.Host)
	log.Printf("end: %s://%s", p.End.Type, p.End.Host)
	listen, err := net.Listen(p.Start.Type, p.Start.Host)
	if err != nil {
		fmt.Println(err)
		return
	}
	for {
		conn, err := listen.Accept()
		if err != nil {
			fmt.Println(err)
			return
		}
		go p.forward(conn)
	}
}
