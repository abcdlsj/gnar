package protocol

import (
	"encoding/json"
	"io"

	"github.com/abcdlsj/pipe/logger"
)

type IMsg interface{}

type AuthMsg struct {
	Token string `json:"token"`
}

type MsgForward struct {
	AuthMsg
	ProxyName  string `json:"proxy_name"`
	SubDomain  string `json:"subdomain"`
	RemotePort int    `json:"remote_port"`
}

func SendForwardMsg(w io.Writer, token, proxyName, subdomain string, remotePort int) error {
	return sendMsg(w, Forward,
		MsgForward{
			AuthMsg: AuthMsg{
				Token: token,
			},
			ProxyName:  proxyName,
			RemotePort: remotePort,
			SubDomain:  subdomain,
		})
}

func ReadForwardMsg(r io.Reader) (MsgForward, error) {
	msg := MsgForward{}

	p, buf, err := ReadMsg(r)
	if err != nil {
		return msg, err
	}
	if p != Forward {
		return msg, ErrInvalidMsg
	}
	if err := json.Unmarshal(buf, &msg); err != nil {
		return msg, err
	}

	return msg, nil
}

type MsgAccept struct {
	AuthMsg
	Domain string `json:"domain"`
	Status string `json:"status"`
}

func SendAcceptMsg(w io.Writer, token, domain, status string) error {
	return sendMsg(w, Accept, MsgAccept{
		AuthMsg: AuthMsg{
			Token: token,
		},
		Domain: domain,
		Status: status,
	})
}

func ReadAccpetMsg(r io.Reader) (MsgAccept, error) {
	msg := MsgAccept{}

	p, buf, err := ReadMsg(r)
	if err != nil {
		return msg, err
	}
	if p != Accept {
		return msg, ErrInvalidMsg
	}
	if err := json.Unmarshal(buf, &msg); err != nil {
		return msg, err
	}

	return msg, nil
}

type MsgExchang struct {
	AuthMsg
	ConnId string `json:"conn_id"`
}

func SendExchangeMsg(w io.Writer, token, connId string) error {
	return sendMsg(w, Exchange, MsgExchang{
		AuthMsg: AuthMsg{
			Token: token,
		},
		ConnId: connId,
	})
}

func ReadExchangeMsg(r io.Reader) (MsgExchang, error) {
	msg := MsgExchang{}

	p, buf, err := ReadMsg(r)
	if err != nil {
		return msg, err
	}
	if p != Exchange {
		return msg, ErrInvalidMsg
	}
	if err := json.Unmarshal(buf, &msg); err != nil {
		return msg, err
	}

	return msg, nil
}

type MsgCancel struct {
	AuthMsg
	ProxyName  string `json:"proxy_name"`
	RemotePort int    `json:"remote_port"`
}

func SendCancelMsg(w io.Writer, token, proxyName string, remotePort int) error {
	return sendMsg(w, Cancel, MsgCancel{
		AuthMsg: AuthMsg{
			Token: token,
		},
		ProxyName:  proxyName,
		RemotePort: remotePort,
	})
}

func ReadCancelMsg(r io.Reader) (MsgCancel, error) {
	msg := MsgCancel{}

	p, buf, err := ReadMsg(r)
	if err != nil {
		return msg, err
	}
	if p != Cancel {
		return msg, ErrInvalidMsg
	}
	if err := json.Unmarshal(buf, &msg); err != nil {
		return msg, err
	}

	return msg, nil
}

type MsgNope struct {
	AuthMsg
}

func sendMsg(w io.Writer, typ PacketType, msg IMsg) error {
	buf, err := packet(typ, msg)
	if err != nil {
		return err
	}
	_, err = w.Write(buf)
	logger.DebugF("Send [%s] msg: [%v]", typ, buf)
	return err
}

func packet(typ PacketType, msg IMsg) ([]byte, error) {
	buf, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return packet0(typ, buf)
}

func packet0(typ PacketType, buf []byte) ([]byte, error) {
	if len(buf) > 65535 {
		return nil, ErrMsgLength
	}
	ret := make([]byte, 3+len(buf))
	ret[0] = byte(typ)
	ret[1] = byte(len(buf) >> 8)
	ret[2] = byte(len(buf))
	copy(ret[3:], buf)
	return ret, nil
}

func ReadMsg(r io.Reader) (PacketType, []byte, error) {
	typ, buf, err := readMsg(r)
	if err != nil {
		return Unknown, nil, err
	}
	return PacketType(typ), buf, nil
}

func readMsg(r io.Reader) (typ byte, buf []byte, err error) {
	buf = make([]byte, 1)
	_, err = r.Read(buf)
	if err != nil {
		return
	}

	typ = buf[0]

	buf = make([]byte, 2)
	_, err = r.Read(buf)
	if err != nil {
		err = ErrMsgRead
		return
	}
	len := int(buf[0])<<8 + int(buf[1])
	buf = make([]byte, len)
	n, err := io.ReadFull(r, buf)
	if err != nil {
		return
	}

	if n != len {
		err = ErrMsgLength
		return
	}

	return
}
