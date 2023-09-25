package client

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/abcdlsj/pipe/protocol"

	"github.com/abcdlsj/pipe/logger"
	"github.com/abcdlsj/pipe/proxy"
)

type Client struct {
	cfg Config
	wg  sync.WaitGroup
}

func newClient(cfg Config) *Client {
	return &Client{
		cfg: cfg,
	}
}

func (c *Client) Run() {
	go c.signalShutdown()

	for _, forward := range c.cfg.Forwards {
		c.wg.Add(1)
		go func(f Forward) {
			defer c.wg.Done()
			c.Handle(f)
		}(forward)
	}

	c.wait()
}

func (c *Client) wait() {
	c.wg.Wait()
}

func (c *Client) Handle(forward Forward) {
	rConn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.cfg.SvrHost, c.cfg.SvrPort))
	if err != nil {
		logger.FatalF("Error connecting to remote: %v", err)
	}

	if err = protocol.NewMsgForward(c.cfg.Token, forward.ProxyName, forward.SubDomain,
		forward.RemotePort).Send(rConn); err != nil {

		logger.FatalF("Error send forward msg to remote: %v", err)
	}

	accept := &protocol.MsgAccept{}
	if err = accept.Recv(rConn); err != nil {
		logger.FatalF("Error read accept msg from remote: %v", err)
	}

	if c.tokenCheck(accept.Token) {
		logger.Fatal("Accept msg token not match")
	}

	if accept.Status != "success" {
		logger.FatalF("Forward failed, status: %s, remote port: %d, domain: %s", accept.Status,
			forward.RemotePort, accept.Domain)
	}

	logger.InfoF("Forward success, remote port: %d, domain: %s", forward.RemotePort, accept.Domain)

	for {
		exMsg := &protocol.MsgExchange{}
		if err = exMsg.Recv(rConn); err != nil {
			logger.ErrorF("Error read exchange msg from remote: %v", err)
			return
		}

		if c.tokenCheck(exMsg.Token) {
			logger.Fatal("Exchange msg token not match")
		}

		logger.InfoF("Receive user req from server, start proxying, conn_id: %s", exMsg.ConnId)
		nRconn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.cfg.SvrHost, c.cfg.SvrPort))
		if err != nil {
			logger.ErrorF("Error connecting to remote: %v", err)
			return
		}

		lConn, err := net.Dial("tcp", fmt.Sprintf(":%d", forward.LocalPort))
		if err != nil {
			logger.ErrorF("Error connecting to local: %v, will close forward, local port: %d", err, forward.LocalPort)
			c.cancelForward(forward)
			return
		}

		go func() {
			if err = protocol.NewMsgExchange(c.cfg.Token, exMsg.ConnId).Send(nRconn); err != nil {
				logger.InfoF("Error sending exchange msg to remote: %v", err)
			}
			proxy.P(lConn, nRconn)
		}()
	}
}

func (c *Client) cancelForward(forward Forward) {
	rConn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.cfg.SvrHost, c.cfg.SvrPort))
	if err != nil {
		logger.FatalF("Error connecting to remote: %v", err)
	}
	if err = protocol.NewMsgCancel(c.cfg.Token, forward.ProxyName, forward.RemotePort).Send(rConn); err != nil {
		logger.FatalF("Error sending cancel msg to remote: %v", err)
	}
	logger.InfoF("Close connection to server, local port: %d, remote port: %d", forward.LocalPort, forward.RemotePort)
}

func (c *Client) tokenCheck(r string) bool {
	return c.cfg.Token != "" && c.cfg.Token != r
}

func (c *Client) signalShutdown() {
	logger.InfoF("Press Ctrl+C to shutdown")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	<-sc
	logger.InfoF("Receive signal to shutdown")

	for _, forward := range c.cfg.Forwards {
		c.cancelForward(forward)
	}

	os.Exit(1)
}
