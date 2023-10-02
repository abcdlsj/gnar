package protocol

import (
	"encoding/json"
	"io"
	"sync"

	"github.com/abcdlsj/pipe/logger"
)

var globalAuthor Authorizator

type Msg interface {
	Send(io.Writer) error
	Recv(io.Reader) error
}

type MsgHeartbeat struct {
	MetaMsg
}

func NewMsgHeartbeat(token string) *MsgHeartbeat {
	return &MsgHeartbeat{
		MetaMsg: newMetaMsg(token),
	}
}

func (m *MsgHeartbeat) Send(w io.Writer) error {
	return sendWith(w, m)
}

func (m *MsgHeartbeat) Recv(r io.Reader) error {
	return recvInto(r, m)
}

type MetaMsg struct {
	Token   string `json:"token"`
	Version string `json:"version"`
}

func newMetaMsg(token string) MetaMsg {
	return MetaMsg{
		Token:   token,
		Version: "0.0.1",
	}
}

type MsgForward struct {
	MetaMsg
	RemotePort int    `json:"remote_port"`
	ProxyName  string `json:"proxy_name"`
	Subdomain  string `json:"subdomain"`
	Type       string `json:"type"`
}

func (m *MsgForward) Send(w io.Writer) error {
	return sendWith(w, m)
}

func (m *MsgForward) Recv(r io.Reader) error {
	return recvInto(r, m)
}

func NewMsgForward(token, proxyName, subdomain string, remotePort int) *MsgForward {
	return &MsgForward{
		MetaMsg:    newMetaMsg(token),
		ProxyName:  proxyName,
		Subdomain:  subdomain,
		RemotePort: remotePort,
	}
}

type MsgForwardResp struct {
	MetaMsg
	Domain string `json:"domain"`
	Status string `json:"status"`
}

func NewMsgForwardResp(token, domain, status string) *MsgForwardResp {
	return &MsgForwardResp{
		MetaMsg: newMetaMsg(token),
		Domain:  domain,
		Status:  status,
	}
}

func (m *MsgForwardResp) Send(w io.Writer) error {
	return sendWith(w, m)
}

func (m *MsgForwardResp) Recv(r io.Reader) error {
	return recvInto(r, m)
}

type MsgExchange struct {
	MetaMsg
	ConnId string `json:"conn_id"`
}

func (m *MsgExchange) Send(w io.Writer) error {
	return sendWith(w, m)
}

func (m *MsgExchange) Recv(r io.Reader) error {
	return recvInto(r, m)
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
	return sendWith(w, m)
}

func (m *MsgCancel) Recv(r io.Reader) error {
	return recvInto(r, m)
}

func Read(r io.Reader) (PacketType, []byte, error) {
	return read(r)
}

func sendWith(w io.Writer, msg Msg) error {
	buf, err := packet(extractMsgType(msg), msg)
	if err != nil {
		return err
	}
	_, err = w.Write(buf)
	return err
}

func recvInto(r io.Reader, msg Msg) error {
	p, buf, err := read(r)
	if err != nil {
		return err
	}

	if p != extractMsgType(msg) {
		return ErrInvalidMsg
	}

	if err := json.Unmarshal(buf, msg); err != nil {
		return err
	}

	meta, err := extractMetaMsg(msg)
	if err != nil {
		return err
	}

	if !globalAuthor.Check(meta.Token) {
		return ErrInvalidToken
	}

	return nil
}

func extractMsgType(msg Msg) PacketType {
	switch msg.(type) {
	case *MsgForward:
		return Forward
	case *MsgForwardResp:
		return ForwardResp
	case *MsgExchange:
		return Exchange
	case *MsgCancel:
		return Cancel
	case *MsgHeartbeat:
		return Heartbeat
	default:
		return Unknown
	}
}

func extractMetaMsg(msg Msg) (MetaMsg, error) {
	switch msg := msg.(type) {
	case *MsgForward:
		return msg.MetaMsg, nil
	case *MsgForwardResp:
		return msg.MetaMsg, nil
	case *MsgExchange:
		return msg.MetaMsg, nil
	case *MsgCancel:
		return msg.MetaMsg, nil
	case *MsgHeartbeat:
		return msg.MetaMsg, nil
	default:
		return newMetaMsg(""), ErrInvalidMsg
	}
}

type Authorizator struct {
	token string
	mu    sync.Mutex
}

func InitAuthorizator(token string) error {
	globalAuthor.token = token
	return nil
}

func (a *Authorizator) Check(token string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.token == "" {
		return true
	}

	if a.token != token {
		logger.Info("Notice: token authentication failed")
		return false
	}

	return true
}
