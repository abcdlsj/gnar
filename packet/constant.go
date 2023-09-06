package packet

import "errors"

type PacketType byte

const Len = 6

var (
	Unknown             = PacketType(0x00)
	Forward  PacketType = PacketType(0x01)
	Accept   PacketType = PacketType(0x02)
	Exchange PacketType = PacketType(0x03)
	Cancel   PacketType = PacketType(0x04)
)

var (
	ErrInvalidMsg   = errors.New("invalid message")
	ErrMsgRead      = errors.New("error reading from connection")
	ErrMsgLength    = errors.New("invalid message length")
	ErrInvalidToken = errors.New("invalid token")
	ErrMsgUnmarshal = errors.New("error unmarshalling message")
)
