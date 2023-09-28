package server

import (
	"net"
	"sync"
	"time"
)

const connIdLen = 8

type Conn struct {
	conn net.Conn
	t    time.Time
}

type ConnMap struct {
	conns map[string]Conn

	mu sync.RWMutex
}

func (c *ConnMap) Add(id string, conn net.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conns[id] = Conn{
		conn: conn,
		t:    time.Now(),
	}
}

func (c *ConnMap) Get(id string) (net.Conn, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	conn, ok := c.conns[id]
	return conn.conn, ok
}

func (c *ConnMap) Del(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.conns, id)
}

func (c *ConnMap) StartAutoExpire() {
	time.AfterFunc(time.Minute, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for id, conn := range c.conns {
			if time.Since(conn.t) > time.Minute {
				delete(c.conns, id)
			}
		}
	})
}
