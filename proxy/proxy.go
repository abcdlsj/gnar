package proxy

import (
	"io"
	"net"
)

func P(src, dst net.Conn) {
	defer src.Close()
	defer dst.Close()

	go io.Copy(src, dst)
	io.Copy(dst, src)
}
