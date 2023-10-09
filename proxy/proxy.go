package proxy

import (
	"io"
)

func Stream(s1, s2 io.ReadWriteCloser) {
	defer s1.Close()
	defer s2.Close()

	copy := func(src io.Reader, dst io.Writer) {
		buf := bufPool.Get().(*Buf)
		defer bufPool.Put(buf)

		for {
			n, err := io.CopyBuffer(dst, src, buf.buf)
			if err == io.EOF || n == 0 {
				break
			}
		}
	}

	go func() {
		copy(s1, s2)
	}()

	copy(s2, s1)
}
