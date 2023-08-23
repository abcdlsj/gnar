package proxy

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type Buf struct {
	buf []byte
}

var bufPool = sync.Pool{
	New: func() interface{} {
		return &Buf{
			buf: make([]byte, 4096),
		}
	},
}

type Traffic struct {
	up int64
	dn int64
	st int64
	et int64
}

func P(src, dst net.Conn) Traffic {
	defer src.Close()
	defer dst.Close()

	buf := bufPool.Get().(*Buf)
	defer bufPool.Put(buf)

	t := Traffic{
		st: time.Now().UnixNano(),
	}

	go func() {
		for {
			n, err := io.CopyBuffer(src, dst, buf.buf)
			if err == io.EOF || n == 0 {
				break
			}
			t.up += n
		}
	}()

	for {
		n, err := io.CopyBuffer(dst, src, buf.buf)
		if err == io.EOF || n == 0 {
			break
		}
		t.dn += n
	}

	t.et = time.Now().UnixNano()

	return t
}

func CalculateBandwidth(traffics []Traffic) (string, string, string) {
	upBytes := int64(0)
	dnBytes := int64(0)
	sumElapsedNano := int64(0)
	for _, t := range traffics {
		upBytes += t.up
		dnBytes += t.dn
		sumElapsedNano += (t.et - t.st)
	}

	avgElapsedTime := float64(sumElapsedNano) / float64(len(traffics)) // nanosecond
	upbw := float64(upBytes) / avgElapsedTime * 1e9
	dnbw := float64(dnBytes) / avgElapsedTime * 1e9

	return humanBytes(upbw) + "/s", humanBytes(dnbw) + "/s",
		humanBytes(float64(upBytes) + float64(dnBytes))
}

func humanBytes(b float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	i := 0
	for b > 1024 {
		b /= 1024
		i++
	}
	return fmt.Sprintf("%.2f%s", b, units[i])
}
