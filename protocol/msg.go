package protocol

import (
	"encoding/json"
	"io"
)

type Msg interface {
	Send(io.Writer) error
	Recv(io.Reader) error
	Unmarshal([]byte) error
	Marshal() ([]byte, error)
}

type MetaMsg struct {
	Token string `json:"token"`
}

func newMetaMsg(token string) MetaMsg {
	return MetaMsg{
		Token: token,
	}
}

type MsgForward struct {
	MetaMsg
	RemotePort int    `json:"remote_port"`
	ProxyName  string `json:"proxy_name"`
	SubDomain  string `json:"subdomain"`
}

func (m *MsgForward) Send(w io.Writer) error {
	return sendMsg(w, Forward, m)
}

func (m *MsgForward) Recv(r io.Reader) error {
	p, buf, err := readMsg(r)
	if err != nil {
		return err
	}
	if p != Forward {
		return ErrInvalidMsg
	}
	return m.Unmarshal(buf)
}

func (m *MsgForward) Unmarshal(buf []byte) error {
	return json.Unmarshal(buf, m)
}

func (m *MsgForward) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func NewMsgForward(token, proxyName, subdomain string, remotePort int) *MsgForward {
	return &MsgForward{
		MetaMsg:    newMetaMsg(token),
		ProxyName:  proxyName,
		SubDomain:  subdomain,
		RemotePort: remotePort,
	}
}

type MsgAccept struct {
	MetaMsg
	Domain string `json:"domain"`
	Status string `json:"status"`
}

func NewMsgAccept(token, domain, status string) *MsgAccept {
	return &MsgAccept{
		MetaMsg: newMetaMsg(token),
		Domain:  domain,
		Status:  status,
	}
}

func (m *MsgAccept) Send(w io.Writer) error {
	return sendMsg(w, Accept, m)
}

func (m *MsgAccept) Recv(r io.Reader) error {
	p, buf, err := readMsg(r)
	if err != nil {
		return err
	}
	if p != Accept {
		return ErrInvalidMsg
	}

	return m.Unmarshal(buf)
}

func (m *MsgAccept) Unmarshal(buf []byte) error {
	return json.Unmarshal(buf, m)
}

func (m *MsgAccept) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

type MsgExchange struct {
	MetaMsg
	ConnId string `json:"conn_id"`
}

func (m *MsgExchange) Send(w io.Writer) error {
	return sendMsg(w, Exchange, m)
}

func (m *MsgExchange) Recv(r io.Reader) error {
	p, buf, err := readMsg(r)
	if err != nil {
		return err
	}
	if p != Exchange {
		return ErrInvalidMsg
	}
	return m.Unmarshal(buf)
}

func (m *MsgExchange) Unmarshal(buf []byte) error {
	return json.Unmarshal(buf, m)
}

func (m *MsgExchange) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func NewMsgExchange(token, connId string) *MsgExchange {
	return &MsgExchange{
		MetaMsg: newMetaMsg(token),
		ConnId:  connId,
	}
}

type MsgCancel struct {
	MetaMsg
	ProxyName  string `json:"proxy_name"`
	RemotePort int    `json:"remote_port"`
}

func NewMsgCancel(token, proxyName string, remotePort int) *MsgCancel {
	return &MsgCancel{
		MetaMsg: MetaMsg{
			Token: token,
		},
		ProxyName:  proxyName,
		RemotePort: remotePort,
	}
}

func (m *MsgCancel) Send(w io.Writer) error {
	return sendMsg(w, Cancel, m)
}

func (m *MsgCancel) Recv(r io.Reader) error {
	p, buf, err := readMsg(r)
	if err != nil {
		return err
	}
	if p != Cancel {
		return ErrInvalidMsg
	}
	return m.Unmarshal(buf)
}

func (m *MsgCancel) Unmarshal(buf []byte) error {
	return json.Unmarshal(buf, m)
}

func (m *MsgCancel) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func ReadMsg(r io.Reader) (PacketType, []byte, error) {
	return readMsg(r)
}
