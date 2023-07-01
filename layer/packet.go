package layer

import (
	"io"
)

type PacketType byte

const Len = 6

var (
	RegisterForward PacketType = PacketType(0x01)
	ExchangeMsg     PacketType = PacketType(0x02)
	CancelForward   PacketType = PacketType(0x03)
)

func ParseRegisterPacket(buf []byte) int {
	return int(buf[1])<<8 + int(buf[2])
}

func ParseCancelPacket(buf []byte) int {
	return int(buf[1])<<8 + int(buf[2])
}

func ParseExchangePacket(buf []byte) string {
	return string(buf[1:])
}

func (p PacketType) Send(w io.Writer, payloads ...interface{}) error {
	var err error

	payload := make([]byte, Len)
	payload[0] = byte(p)

	switch p {
	case RegisterForward:
		uport := payloads[0].(int)
		payload[1] = byte(uport >> 8)
		payload[2] = byte(uport)

		_, err = w.Write(payload)

	case ExchangeMsg:
		cid := payloads[0].(string)
		copy(payload[1:], []byte(cid))

		_, err = w.Write(payload)

	case CancelForward:
		uport := payloads[0].(int)
		payload[1] = byte(uport >> 8)
		payload[2] = byte(uport)

		_, err = w.Write(payload)
	}

	return err
}

func Read(r io.Reader) (PacketType, []byte, error) {
	buf := make([]byte, Len)
	_, err := r.Read(buf)
	if err != nil {
		return 0, nil, err
	}

	return PacketType(buf[0]), buf, nil
}
