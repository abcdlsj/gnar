package protocol

import (
	"encoding/json"
	"fmt"
	"io"
)

// Packet types.
const (
	PacketTypeAuth       byte = 0x01
	PacketTypeAuthResp   byte = 0x02
	PacketTypeTunnelReq  byte = 0x10
	PacketTypeTunnelResp byte = 0x11
	PacketTypeData       byte = 0x20
	PacketTypeHeartbeat  byte = 0x30
	PacketTypeClose      byte = 0x40
)

// Packet is the base interface for all packets.
type Packet interface {
	Type() byte
}

// AuthPacket is sent by client to authenticate.
type AuthPacket struct {
	AccessToken string `json:"access_token"`
	Version     string `json:"version"`
}

func (p AuthPacket) Type() byte { return PacketTypeAuth }

// AuthResponse is sent by server in response to auth.
type AuthResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

func (p AuthResponse) Type() byte { return PacketTypeAuthResp }

// TunnelRequest is sent by client to create a tunnel.
type TunnelRequest struct {
	ReqID     string `json:"req_id"`
	LocalPort int    `json:"local_port"`
	Subdomain string `json:"subdomain,omitempty"`
	Protocol  string `json:"protocol"`
}

func (p TunnelRequest) Type() byte { return PacketTypeTunnelReq }

// TunnelResponse is sent by server to confirm tunnel creation.
type TunnelResponse struct {
	ReqID      string `json:"req_id"`
	Success    bool   `json:"success"`
	TunnelID   string `json:"tunnel_id,omitempty"`
	PublicURL  string `json:"public_url,omitempty"`
	ServerPort int    `json:"server_port,omitempty"`
	Error      string `json:"error,omitempty"`
}

func (p TunnelResponse) Type() byte { return PacketTypeTunnelResp }

// DataPacket wraps HTTP data.
type DataPacket struct {
	TunnelID string `json:"tunnel_id"`
	Data     []byte `json:"data"`
}

func (p DataPacket) Type() byte { return PacketTypeData }

// HeartbeatPacket keeps connection alive.
type HeartbeatPacket struct {
	Timestamp int64 `json:"timestamp"`
}

func (p HeartbeatPacket) Type() byte { return PacketTypeHeartbeat }

// ClosePacket signals tunnel closure.
type ClosePacket struct {
	TunnelID string `json:"tunnel_id"`
	Reason   string `json:"reason,omitempty"`
}

func (p ClosePacket) Type() byte { return PacketTypeClose }

// Encoder encodes packets to a writer.
type Encoder struct {
	w io.Writer
}

// NewEncoder creates a new encoder.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

// Encode writes a packet to the writer.
// Format: [1 byte type][4 bytes length][JSON data]
func (e *Encoder) Encode(p Packet) error {
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal packet: %w", err)
	}

	// Write type
	if _, err := e.w.Write([]byte{p.Type()}); err != nil {
		return fmt.Errorf("write type: %w", err)
	}

	// Write length (big endian)
	length := uint32(len(data))
	lenBuf := []byte{
		byte(length >> 24),
		byte(length >> 16),
		byte(length >> 8),
		byte(length),
	}
	if _, err := e.w.Write(lenBuf); err != nil {
		return fmt.Errorf("write length: %w", err)
	}

	// Write data
	if _, err := e.w.Write(data); err != nil {
		return fmt.Errorf("write data: %w", err)
	}

	return nil
}

// Decoder decodes packets from a reader.
type Decoder struct {
	r io.Reader
}

// NewDecoder creates a new decoder.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: r}
}

// Decode reads and decodes a packet.
func (d *Decoder) Decode() (Packet, byte, error) {
	// Read type
	typeBuf := make([]byte, 1)
	if _, err := io.ReadFull(d.r, typeBuf); err != nil {
		return nil, 0, fmt.Errorf("read type: %w", err)
	}
	pktType := typeBuf[0]

	// Read length
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(d.r, lenBuf); err != nil {
		return nil, 0, fmt.Errorf("read length: %w", err)
	}
	length := uint32(lenBuf[0])<<24 | uint32(lenBuf[1])<<16 | uint32(lenBuf[2])<<8 | uint32(lenBuf[3])

	// Read data
	data := make([]byte, length)
	if _, err := io.ReadFull(d.r, data); err != nil {
		return nil, 0, fmt.Errorf("read data: %w", err)
	}

	// Unmarshal based on type
	var pkt Packet
	switch pktType {
	case PacketTypeAuth:
		pkt = &AuthPacket{}
	case PacketTypeAuthResp:
		pkt = &AuthResponse{}
	case PacketTypeTunnelReq:
		pkt = &TunnelRequest{}
	case PacketTypeTunnelResp:
		pkt = &TunnelResponse{}
	case PacketTypeData:
		pkt = &DataPacket{}
	case PacketTypeHeartbeat:
		pkt = &HeartbeatPacket{}
	case PacketTypeClose:
		pkt = &ClosePacket{}
	default:
		return nil, pktType, fmt.Errorf("unknown packet type: %d", pktType)
	}

	if err := json.Unmarshal(data, pkt); err != nil {
		return nil, pktType, fmt.Errorf("unmarshal packet: %w", err)
	}

	return pkt, pktType, nil
}
