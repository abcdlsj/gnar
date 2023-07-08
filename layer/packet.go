package layer

import (
	"io"
)

func (p PacketType) Send(w io.Writer, payloads ...interface{}) error {
	var msg IMsg
	switch p {
	case RegisterForward:
		msg = MsgNewProxy{
			AuthMsg: AuthMsg{
				Token: payloads[0].(string),
			},
			ProxyName:  payloads[1].(string),
			RemotePort: payloads[2].(int),
			SubDomain:  payloads[3].(string),
		}
	case ExchangeMsg:
		msg = MsgExchange{
			AuthMsg: AuthMsg{
				Token: payloads[0].(string),
			},
			ConnId: payloads[1].(string),
		}
	case CancelForward:
		msg = MsgCancelProxy{
			AuthMsg: AuthMsg{
				Token: payloads[0].(string),
			},
			ProxyName:  payloads[1].(string),
			RemotePort: payloads[2].(int),
		}
	}

	return sendMsg(w, p, msg)
}

func Read(r io.Reader) (PacketType, []byte, error) {
	buf := make([]byte, Len)
	_, err := r.Read(buf)
	if err != nil {
		return 0, nil, err
	}

	return PacketType(buf[0]), buf, nil
}
