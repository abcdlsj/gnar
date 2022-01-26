package layer

type PacketType byte

const Len = 6

var (
	RegisterForward PacketType = PacketType(0x01)
	ExchangeMsg     PacketType = PacketType(0x02)
	CancelForward   PacketType = PacketType(0x03)
)
