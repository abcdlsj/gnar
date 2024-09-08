package conn

import (
	"io"
	"net"
	"sync"
	"time"
)

type TCPConn struct {
	t    time.Time
	conn io.ReadWriteCloser
}

type TCPConnMap struct {
	conns map[string]TCPConn
	mu    sync.RWMutex
}

func NewTCPConnMap() TCPConnMap {
	return TCPConnMap{
		conns: make(map[string]TCPConn),
	}
}

func (c *TCPConnMap) Add(id string, conn net.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conns[id] = TCPConn{
		conn: conn,
		t:    time.Now(),
	}
}

func (c *TCPConnMap) Get(id string) (io.ReadWriteCloser, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	conn, ok := c.conns[id]
	return conn.conn, ok
}

func (c *TCPConnMap) Del(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.conns, id)
}

func (c *TCPConnMap) StartAutoExpire() {
	expire := func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for id, conn := range c.conns {
			if time.Since(conn.t) > time.Second*10 {
				delete(c.conns, id)
			}
		}
	}

	ticker := time.NewTicker(time.Second * 10)
	for range ticker.C {
		expire()
	}
}
