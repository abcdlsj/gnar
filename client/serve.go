package client

import (
	"fmt"
	"github.com/abcdlsj/pipe/protocol"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/abcdlsj/pipe/logger"
	"github.com/abcdlsj/pipe/proxy"
)

type Client struct {
	cfg Config
}

func newClient(cfg Config) *Client {
	return &Client{
		cfg: cfg,
	}
}

func (c *Client) Run() {
	go c.signalShutdown()

	for _, forward := range c.cfg.Forwards {
		forward := forward
		go func() {
			rConn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.cfg.SvrHost, c.cfg.SvrPort))
			if err != nil {
				logger.ErrorF("Error connecting to remote: %v", err)
			}
			if err := protocol.SendForwardMsg(rConn, c.cfg.Token, forward.ProxyName,
				forward.SubDomain, forward.RemotePort); err != nil {

				logger.FatalF("Error send forward msg to remote: %v", err)
			}

			accept, err := protocol.ReadAccpetMsg(rConn)
			if err != nil {
				logger.FatalF("Error read accept msg from remote: %v", err)
			}

			if c.tokenCheck(accept.Token) {
				logger.FatalF("Accept msg token not match: [%s]", accept.Token)
			}

			if accept.Status != "success" {
				logger.FatalF("Forward failed, status: %s, remote port: %d, domain: %s", accept.Status, forward.RemotePort, accept.Domain)
			}

			logger.InfoF("Forward success, remote port: %d, domain: %s", forward.RemotePort, accept.Domain)

			for {
				exchange, err := protocol.ReadExchangeMsg(rConn)
				if err != nil {
					logger.FatalF("Error read exchange msg from remote: %v", err)
				}

				if c.tokenCheck(exchange.Token) {
					logger.FatalF("Exchange msg token not match: [%s]", exchange.Token)
				}

				logger.InfoF("Receive user req from server, start proxying, conn_id: %s", exchange.ConnId)
				nRconn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.cfg.SvrHost, c.cfg.SvrPort))
				if err != nil {
					logger.ErrorF("Error connecting to remote: %v", err)
					return
				}

				lConn, err := net.Dial("tcp", fmt.Sprintf(":%d", forward.LocalPort))
				if err != nil {
					logger.ErrorF("Error connecting to local: %v, will close proxy, port: %d", err, forward.LocalPort)
					if err := protocol.SendCancelMsg(rConn, c.cfg.Token, forward.ProxyName, forward.RemotePort); err != nil {
						logger.FatalF("Error sending cancel msg to remote: %v, to close proxy, port: %d", err, forward.LocalPort)
					}
					return
				}

				go func() {
					if err := protocol.SendExchangeMsg(nRconn, c.cfg.Token, exchange.ConnId); err != nil {
						logger.InfoF("Error sending exchange msg to remote: %v", err)
					}
					proxy.P(lConn, nRconn)
				}()
			}
		}()
	}

	select {}
}

func (c *Client) CancelForward() {
	for _, forward := range c.cfg.Forwards {
		nRconn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.cfg.SvrHost, c.cfg.SvrPort))
		if err != nil {
			logger.FatalF("Error connecting to remote: %v", err)
		}
		if err := protocol.SendCancelMsg(nRconn, c.cfg.Token, forward.ProxyName, forward.RemotePort); err != nil {
			logger.FatalF("Error sending cancel msg to remote: %v", err)
		}
		logger.InfoF("Close connection to server, remote port: %d", forward.RemotePort)
	}
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
	c.CancelForward()
	os.Exit(1)
}
