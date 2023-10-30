package proto

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/abcdlsj/pipe/share"
)

type Msg interface {
	Type() PacketType
}

func Send(w io.Writer, msg Msg) error {
	buf, err := packet(msg.Type(), msg)
	if err != nil {
		return err
	}
	_, err = w.Write(buf)
	return err
}

func Recv(r io.Reader, msg Msg) error {
	p, buf, err := read(r)
	if err != nil {
		return err
	}

	if p != msg.Type() {
		return ErrInvalidMsg
	}

	if err := json.Unmarshal(buf, msg); err != nil {
		return err
	}

	return nil
}

func Read(r io.Reader) (PacketType, []byte, error) {
	return read(r)
}

type MsgHeartbeat struct{}

func (m *MsgHeartbeat) Type() PacketType {
	return PacketHeartbeat
}

func NewMsgHeartbeat() *MsgHeartbeat {
	return &MsgHeartbeat{}
}

type MsgLogin struct {
	Token     string `json:"token"`
	Version   string `json:"version"`
	Timestamp int64  `json:"timestamp"`
}

func (m *MsgLogin) Type() PacketType {
	return PacketLogin
}

func NewMsgLogin(token string) *MsgLogin {
	ts := time.Now().Unix()
	hash := md5.New()
	hash.Write([]byte(token + fmt.Sprintf("%d", ts)))

	return &MsgLogin{
		Token:     fmt.Sprintf("%x", hash.Sum(nil)),
		Version:   share.GetVersion(),
		Timestamp: ts,
	}
}

type MsgForwardReq struct {
	RemotePort int    `json:"remote_port"`
	ProxyName  string `json:"proxy_name"`
	Subdomain  string `json:"subdomain"`
	ProxyType  string `json:"proxy_type"`
}

func (m *MsgForwardReq) Type() PacketType {
	return PacketForwardReq
}

func NewMsgForward(proxyName, subdomain, proxyType string, remotePort int) *MsgForwardReq {
	return &MsgForwardReq{
		ProxyName:  proxyName,
		Subdomain:  subdomain,
		RemotePort: remotePort,
		ProxyType:  proxyType,
	}
}

type MsgForwardResp struct {
	Domain string `json:"domain"`
	Status string `json:"status"`
}

func (m *MsgForwardResp) Type() PacketType {
	return PacketForwardResp
}

func NewMsgForwardResp(domain, status string) *MsgForwardResp {
	return &MsgForwardResp{
		Domain: domain,
		Status: status,
	}
}

type MsgForwardCancel struct {
	ProxyName  string `json:"proxy_name"`
	RemotePort int    `json:"remote_port"`
}

func NewMsgCancel(token, proxyName string, remotePort int) *MsgForwardCancel {
	return &MsgForwardCancel{
		ProxyName:  proxyName,
		RemotePort: remotePort,
	}
}

func (m *MsgForwardCancel) Type() PacketType {
	return PacketForwardCancel
}

type MsgExchange struct {
	ConnId    string `json:"conn_id"`
	ProxyType string `json:"proxy_type"`
}

func (m *MsgExchange) Type() PacketType {
	return PacketExchange
}

func NewMsgExchange(connId, typ string) *MsgExchange {
	return &MsgExchange{
		ConnId:    connId,
		ProxyType: typ,
	}
}

type MsgUDPDatagram struct {
	Payload []byte       `json:"payload"`
	Addr    *net.UDPAddr `json:"addr"`
}

func (m *MsgUDPDatagram) Type() PacketType {
	return PacketUDPDatagram
}

func NewMsgUDPDatagram(addr *net.UDPAddr, payload []byte) *MsgUDPDatagram {
	return &MsgUDPDatagram{
		Payload: payload,
		Addr:    addr,
	}
}
