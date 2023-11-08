package control

import (
	"net"

	"github.com/abcdlsj/pipe/proto"
	"github.com/hashicorp/yamux"
)

type Dialer interface {
	OpenSvrConn() (net.Conn, error)
}

type TCPDialer struct {
	SvrAddr string
	Token   string
}

func NewTCPDialer(addr, token string) *TCPDialer {
	return &TCPDialer{
		SvrAddr: addr,
		Token:   token,
	}
}

func (t *TCPDialer) OpenSvrConn() (net.Conn, error) {
	conn, err := net.Dial("tcp", t.SvrAddr)
	if err != nil {
		return nil, err
	}

	if err = proto.Send(conn, proto.NewMsgLogin(t.Token)); err != nil {
		return nil, err
	}

	return conn, nil
}

type MuxDialer struct {
	SvrAddr string
	Token   string
	Session *yamux.Session
}

func NewMuxDialer(addr, token string) *MuxDialer {
	return &MuxDialer{
		SvrAddr: addr,
		Token:   token,
	}
}

func (m *MuxDialer) OpenSvrConn() (net.Conn, error) {
	if m.Session == nil {
		conn, err := net.Dial("tcp", m.SvrAddr)
		if err != nil {
			return nil, err
		}

		if err = proto.Send(conn, proto.NewMsgLogin(m.Token)); err != nil {
			return nil, err
		}

		session, err := yamux.Client(conn, nil)
		if err != nil {
			return nil, err
		}

		m.Session = session
	}

	return m.Session.Open()
}
