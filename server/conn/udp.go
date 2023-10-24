package conn

import (
	"net"
	"sync"
)

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
