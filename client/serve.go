package client

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/abcdlsj/pipe/logger"
	"github.com/abcdlsj/pipe/protocol"
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

	if c.cfg.Token != "" {
		protocol.InitAuthorizator(c.cfg.Token)
	}

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

	frdResp := &protocol.MsgForwardResp{}
	if err = frdResp.Recv(rConn); err != nil {
		logger.FatalF("Error read forward resp msg from remote: %v", err)
	}

	if frdResp.Status != "success" {
		logger.FatalF("Forward failed, status: %s, remote port: %d, domain: %s", frdResp.Status,
			forward.RemotePort, frdResp.Domain)
	}

	if frdResp.Domain != "" {
		logger.InfoF("Forward success, remote port: %d, domain: %s", forward.RemotePort, frdResp.Domain)
	} else {
		logger.InfoF("Forward success, remote port: %d", forward.RemotePort)
	}

	for {
		p, buf, err := protocol.Read(rConn)
		if err != nil {
			logger.ErrorF("Error read msg from remote: %v", err)
			return
		}
		switch p {
		case protocol.Exchange:
			msg := &protocol.MsgExchange{}
			if err := json.Unmarshal(buf, msg); err != nil {
				logger.ErrorF("Error read exchange msg from remote: %v", err)
				c.cancelForward(forward)
				return
			}

			logger.InfoF("Receive user req from server, start proxying, conn_id: %s", msg.ConnId)
			lConn, err := net.Dial("tcp", fmt.Sprintf(":%d", forward.LocalPort))
			if err != nil {
				logger.ErrorF("Error connecting to local: %v, will close forward, local port: %d", err, forward.LocalPort)
				c.cancelForward(forward)
				return
			}

			nRconn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.cfg.SvrHost, c.cfg.SvrPort))
			if err != nil {
				logger.ErrorF("Error connecting to remote: %v", err)
				c.cancelForward(forward)
				return
			}

			go func() {
				if err = protocol.NewMsgExchange(c.cfg.Token, msg.ConnId).Send(nRconn); err != nil {
					logger.InfoF("Error sending exchange msg to remote: %v", err)
				}
				proxy.P(lConn, nRconn)
			}()
		case protocol.Heartbeat:
			msg := &protocol.MsgHeartbeat{}
			if err := json.Unmarshal(buf, msg); err != nil {
				logger.ErrorF("Error read heartbeat msg from remote: %v", err)
				c.cancelForward(forward)
				return
			}
			if !c.sameToken(msg.Token) {
				logger.ErrorF("Receive heartbeat from server, token not match")
				c.cancelForward(forward)
				return
			}
			logger.Debug("Receive heartbeat from server")
		}
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

func (c *Client) sameToken(token string) bool {
	return c.cfg.Token == "" || c.cfg.Token == token
}
