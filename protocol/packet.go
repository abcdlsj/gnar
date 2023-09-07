package protocol

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
