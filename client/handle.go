package client

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/abcdlsj/gpipe/layer"
	"github.com/abcdlsj/gpipe/logger"
	"github.com/abcdlsj/gpipe/proxy"
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
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	go func() {
		<-sc
		c.CancelForward()
		os.Exit(1)
	}()

	for _, forward := range c.cfg.Forwards {
		forward := forward
		go func() {
			rConn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.cfg.ServerHost, c.cfg.ServerPort))
			if err != nil {
				logger.ErrorF("Error connecting to remote: %v", err)
			}
			if err := layer.RegisterForward.Send(rConn, buildMsg(layer.RegisterForward, c.cfg, forward)...); err != nil {
				logger.ErrorF("Error writing to remote: %v", err)
			}

			for {
				p, buf, err := layer.ReadMsg(rConn)
				if err != nil {
					logger.ErrorF("Error reading from connection: %v", err) // FIXME: if connection timeout, will cause error
					return
				}
				if p != layer.ExchangeMsg {
					logger.ErrorF("Unexpected packet type: %v", p)
					return
				}

				msg := &layer.MsgExchange{}
				if err := json.Unmarshal(buf, msg); err != nil {
					logger.ErrorF("Error unmarshal msg: %v", err)
					return
				}

				if c.cfg.Token != "" && msg.Token != c.cfg.Token {
					logger.ErrorF("Token not match: %s", msg.Token)
					return
				}

				logger.Debug("Receive req from server, start proxying")
				nRonn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.cfg.ServerHost, c.cfg.ServerPort))
				if err != nil {
					logger.ErrorF("Error connecting to remote: %v", err)
					return
				}

				lConn, err := net.Dial("tcp", fmt.Sprintf(":%d", forward.LocalPort))
				if err != nil {
					logger.ErrorF("Error connecting to local: %v", err)
					if err := layer.CancelForward.Send(nRonn, buildMsg(layer.CancelForward, c.cfg, forward)...); err != nil {
						logger.FatalF("Error writing to remote: %v, to close proxy, port: %d", err, forward.LocalPort)
					}
					return
				}

				go func() {
					if err := layer.ExchangeMsg.Send(nRonn, c.cfg.Token, msg.ConnId); err != nil {
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
		nRonn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.cfg.ServerHost, c.cfg.ServerPort))
		if err != nil {
			logger.FatalF("Error connecting to remote: %v", err)
		}
		if err := layer.CancelForward.Send(nRonn, buildMsg(layer.CancelForward, c.cfg, forward)...); err != nil {
			logger.FatalF("Error writing to remote: %v", err)
		}
		logger.InfoF("Close connection to server, remote port: %d", forward.RemotePort)
	}
}

func buildMsg(t layer.PacketType, cfg Config, f Forward) []interface{} {
	switch t {
	case layer.RegisterForward:
		return []interface{}{cfg.Token, f.ProxyName, f.RemotePort, f.SubDomain}
	case layer.CancelForward:
		return []interface{}{cfg.Token, f.ProxyName, f.RemotePort}
	}
	return nil
}
