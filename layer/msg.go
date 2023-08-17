package layer

import (
	"encoding/json"
	"io"

	"github.com/abcdlsj/pipe/logger"
)

type IMsg interface{}

type AuthMsg struct {
	Token string `json:"token"`
}

type MsgNewProxy struct {
	AuthMsg
	ProxyName  string `json:"proxy_name"`
	SubDomain  string `json:"subdomain"`
	RemotePort int    `json:"remote_port"`
}

type MsgExchange struct {
	AuthMsg
	ConnId string `json:"conn_id"`
}

type MsgCancelProxy struct {
	AuthMsg
	ProxyName  string `json:"proxy_name"` // optional
	RemotePort int    `json:"remote_port"`
}

func sendMsg(w io.Writer, typ PacketType, msg IMsg) error {
	buf, err := packet(typ, msg)
	if err != nil {
		return err
	}
	_, err = w.Write(buf)
	logger.DebugF("send msg: [%v]", buf)
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
