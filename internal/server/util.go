package server

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

const (
	httpStatusClientCanceled = 499
	httpStatusTunnelClosed   = 598
)

func nextID() string {
	var buf [8]byte
	_, err := rand.Read(buf[:])
	if err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("150405.000000")))
	}
	return hex.EncodeToString(buf[:])
}

func timeAfter(timeout time.Duration) <-chan time.Time {
	return time.After(timeout)
}
