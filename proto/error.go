package proto

import "errors"

var (
	ErrInvalidMsg   = errors.New("invalid message")
	ErrMsgRead      = errors.New("error reading from connection")
	ErrMsgLength    = errors.New("invalid message length")
	ErrInvalidToken = errors.New("invalid token")
	ErrMsgUnmarshal = errors.New("error unmarshalling message")
)
