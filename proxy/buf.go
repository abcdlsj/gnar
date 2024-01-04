package proxy

import "sync"

type Buf struct {
	buf []byte
}

var bufPool = newBufPool(512) // TODO: benchmark this?

func newBufPool(size int) *sync.Pool {
	return &sync.Pool{
		New: func() any {
			return &Buf{
				buf: make([]byte, size),
			}
		},
	}
}
