package protocol

import (
	"encoding/json"
	"io"
)

type PacketType byte

var (
	Unknown                = PacketType(0x00)
	Heartbeat              = PacketType(0x01)
	Forward     PacketType = PacketType(0x02)
	ForwardResp PacketType = PacketType(0x03)
	Exchange    PacketType = PacketType(0x04)
	Cancel      PacketType = PacketType(0x05)
)

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

func read(r io.Reader) (PacketType, []byte, error) {
	typ, buf, err := read0(r)
	if err != nil {
		return Unknown, nil, err
	}
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
