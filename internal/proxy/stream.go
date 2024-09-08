package proxy

import (
	"io"
)

func Stream(s1, s2 io.ReadWriteCloser) {
	s1 = rwcWrap(s1)
	s2 = rwcWrap(s2)

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

// rwcWrap Remove io.ReaderFrom and io.WriterTo from io.ReadWriteCloser (https://github.com/golang/go/issues/16474)
func rwcWrap(rwc io.ReadWriteCloser) io.ReadWriteCloser {
	return struct {
		io.ReadWriteCloser
	}{
		rwc,
	}
}
