package server

import (
	"net"
	"sync"
)

type ConnMap struct {
	conns map[string]net.Conn

	mu sync.RWMutex
}

func (c *ConnMap) Add(id string, conn net.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conns[id] = conn
}

func (c *ConnMap) Get(id string) (net.Conn, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	conn, ok := c.conns[id]
	return conn, ok
}

func (c *ConnMap) Del(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.conns, id)
}
