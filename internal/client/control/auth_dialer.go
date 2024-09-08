package control

import (
	"net"

	"github.com/abcdlsj/gnar/pkg/proto"
	"github.com/hashicorp/yamux"
)

type AuthSvrDialer interface {
	Open() (net.Conn, error)
}

type TCPDialer struct {
	addr  string
	token string
}

func NewTCPDialer(addr, token string) *TCPDialer {
	return &TCPDialer{
		addr:  addr,
		token: token,
	}
}

func (t *TCPDialer) Open() (net.Conn, error) {
	conn, err := net.Dial("tcp", t.addr)
	if err != nil {
		return nil, err
	}

	if err = proto.Send(conn, proto.NewMsgLogin(t.token)); err != nil {
		return nil, err
	}

	return conn, nil
}

type MuxDialer struct {
	addr    string
	token   string
	session *yamux.Session
}

func NewMuxDialer(addr, token string) *MuxDialer {
	return &MuxDialer{
		addr:  addr,
		token: token,
	}
}

func (m *MuxDialer) Open() (net.Conn, error) {
	if m.session == nil {
		conn, err := net.Dial("tcp", m.addr)
		if err != nil {
			return nil, err
		}

		if err = proto.Send(conn, proto.NewMsgLogin(m.token)); err != nil {
			return nil, err
		}

		session, err := yamux.Client(conn, nil)
		if err != nil {
			return nil, err
		}

		m.session = session
	}

	return m.session.Open()
}
