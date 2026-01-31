package server

import (
	"io"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
)

func NewUuid() string {
	return uuid.New().String()[:8]
}

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
	c.conns[id] = TCPConn{conn: conn, t: time.Now()}
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
	ticker := time.NewTicker(10 * time.Second)
	for range ticker.C {
		c.mu.Lock()
		for id, conn := range c.conns {
			if time.Since(conn.t) > 10*time.Second {
				delete(c.conns, id)
			}
		}
		c.mu.Unlock()
	}
}

type UDPConnMap struct {
	conns map[string]*net.UDPConn
	mu    sync.Mutex
}

func NewUDPConnMap() UDPConnMap {
	return UDPConnMap{
		conns: make(map[string]*net.UDPConn),
	}
}

func (c *UDPConnMap) Add(id string, conn *net.UDPConn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conns[id] = conn
}

func (c *UDPConnMap) Get(id string) (*net.UDPConn, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	conn, ok := c.conns[id]
	return conn, ok
}

func (c *UDPConnMap) Del(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conns[id].Close()
	delete(c.conns, id)
}
