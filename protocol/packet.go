package protocol

import (
	"encoding/json"
	"github.com/abcdlsj/pipe/logger"
	"io"
)

type PacketType byte

var (
	Unknown             = PacketType(0x00)
	Forward  PacketType = PacketType(0x01)
	Accept   PacketType = PacketType(0x02)
	Exchange PacketType = PacketType(0x03)
	Cancel   PacketType = PacketType(0x04)
)

func (p PacketType) String() string {
	switch p {
	case Forward:
		return "forward"
	case Accept:
		return "accept"
	case Exchange:
		return "exchange"
	case Cancel:
		return "cancel"
	default:
		return "unknown"
	}
}

func sendMsg(w io.Writer, typ PacketType, msg Msg) error {
	buf, err := packet(typ, msg)
	if err != nil {
		return err
	}
	_, err = w.Write(buf)
	logger.DebugF("[Protocol] Send [%s] msg: [%v]", typ, msg)
	return err
}

func packet(typ PacketType, msg Msg) ([]byte, error) {
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

func readMsg(r io.Reader) (PacketType, []byte, error) {
	typ, buf, err := read0(r)
	if err != nil {
		return Unknown, nil, err
	}
	logger.DebugF("[Protocol] Receive [%s] msg: [%v]", PacketType(typ), buf)
	return PacketType(typ), buf, nil
}

func read0(r io.Reader) (typ byte, buf []byte, err error) {
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
	l := int(buf[0])<<8 + int(buf[1])
	buf = make([]byte, l)
	n, err := io.ReadFull(r, buf)
	if err != nil {
		return
	}

	if n != l {
		err = ErrMsgLength
		return
	}

	return
}
