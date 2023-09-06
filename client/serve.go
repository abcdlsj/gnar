package client

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/abcdlsj/pipe/logger"
	"github.com/abcdlsj/pipe/packet"
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
			if err := packet.Forward.Send(rConn, buildMsg(packet.Forward, c.cfg, forward)...); err != nil {
				logger.ErrorF("Error writing to remote: %v", err)
			}

			for {
				p, buf, err := packet.ReadMsg(rConn)
				if err != nil {
					logger.ErrorF("Error reading from connection: %v", err) // FIXME: if connection timeout, will cause error
					return
				}

				if p == packet.Accept {
					msg := &packet.MsgAccept{}
					if err := json.Unmarshal(buf, msg); err != nil {
						logger.ErrorF("Error unmarshal msg: %v", err)
						return
					}

					if c.tokenCheck(msg.Token) {
						logger.FatalF("Token not match: [%s]", msg.Token)
						return
					}

					if msg.Status != "success" {
						logger.FatalF("Forward failed, status: %s, remote port: %d, domain: %s", msg.Status, forward.RemotePort, msg.Domain)
						return
					}

					logger.InfoF("Forward success, remote port: %d, domain: %s", forward.RemotePort, msg.Domain)
					continue
				}

				if p != packet.Exchange {
					logger.ErrorF("Unexpected packet type: %v", p)
					return
				}

				msg := &packet.MsgExchang{}
				if err := json.Unmarshal(buf, msg); err != nil {
					logger.ErrorF("Error unmarshal msg: %v", err)
					return
				}

				if c.tokenCheck(msg.Token) {
					logger.FatalF("Token not match: [%s]", msg.Token)
					return
				}

				logger.Debug("Receive req from server, start proxying")
				nRonn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.cfg.SvrHost, c.cfg.SvrPort))
				if err != nil {
					logger.ErrorF("Error connecting to remote: %v", err)
					return
				}

				lConn, err := net.Dial("tcp", fmt.Sprintf(":%d", forward.LocalPort))
				if err != nil {
					logger.ErrorF("Error connecting to local: %v", err)
					if err := packet.Cancel.Send(nRonn, buildMsg(packet.Cancel, c.cfg, forward)...); err != nil {
						logger.FatalF("Error writing to remote: %v, to close proxy, port: %d", err, forward.LocalPort)
					}
					return
				}

				go func() {
					if err := packet.Exchange.Send(nRonn, c.cfg.Token, msg.ConnId); err != nil {
						logger.InfoF("Error writing to remote: %v", err)
					}
					proxy.P(lConn, nRonn)
				}()
			}
		}()
	}

	select {}
}

func (c *Client) CancelForward() {
	for _, forward := range c.cfg.Forwards {
		nRonn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.cfg.SvrHost, c.cfg.SvrPort))
		if err != nil {
			logger.FatalF("Error connecting to remote: %v", err)
		}
		if err := packet.Cancel.Send(nRonn, buildMsg(packet.Cancel, c.cfg, forward)...); err != nil {
			logger.FatalF("Error writing to remote: %v", err)
		}
		logger.InfoF("Close connection to server, remote port: %d", forward.RemotePort)
	}
}

func buildMsg(t packet.PacketType, cfg Config, f Forward) []interface{} {
	switch t {
	case packet.Forward:
		return []interface{}{cfg.Token, f.ProxyName, f.RemotePort, f.SubDomain}
	case packet.Cancel:
		return []interface{}{cfg.Token, f.ProxyName, f.RemotePort}
	}
	return nil
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
