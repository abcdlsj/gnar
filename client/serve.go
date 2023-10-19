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
	"github.com/abcdlsj/pipe/pio"
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

func (c *Client) newSvrConn() (net.Conn, error) {
	return net.Dial("tcp", fmt.Sprintf("%s:%d", c.cfg.SvrHost, c.cfg.SvrPort))
}

func (c *Client) Handle(forward Forward) {
	flogger := logger.New(fmt.Sprintf("F<%d:%d>", forward.LocalPort, forward.RemotePort))
	rConn, err := c.newSvrConn()
	if err != nil {
		logger.LFatalF(flogger, "Error connecting to remote: %v", err)
	}

	if err = protocol.NewMsgForward(c.cfg.Token, forward.ProxyName, forward.Subdomain,
		forward.Type, forward.RemotePort).Send(rConn); err != nil {

		logger.LFatalF(flogger, "Error send forward msg to remote: %v", err)
	}

	frdResp := &protocol.MsgForwardResp{}
	if err = frdResp.Recv(rConn); err != nil {
		logger.LFatalF(flogger, "Error read forward resp msg from remote: %v", err)
	}

	if frdResp.Status != "success" {
		logger.LFatalF(flogger, "Forward failed, status: %s, remote port: %d, domain: %s", frdResp.Status,
			forward.RemotePort, frdResp.Domain)
	}

	if frdResp.Domain != "" {
		logger.LInfoF(flogger, "Forward success, remote port: %d, domain: %s", forward.RemotePort, frdResp.Domain)
	} else {
		logger.LInfoF(flogger, "Forward success, remote port: %d", forward.RemotePort)
	}

	for {
		p, buf, err := protocol.Read(rConn)
		if err != nil {
			logger.LErrorF(flogger, "Error read msg from remote: %v", err)
			return
		}
		switch p {
		case protocol.Exchange:
			msg := &protocol.MsgExchange{}
			if err := json.Unmarshal(buf, msg); err != nil {
				logger.LErrorF(flogger, "Error read exchange msg from remote: %v", err)
				c.cancelForward(flogger, forward)
				return
			}

			switch msg.Type {
			case "udp":
				go func() {
					logger.LInfoF(flogger, "Receive udp conn from server, start proxying, conn_id: %s", msg.ConnId)
					nRconn, err := c.newSvrConn()
					if err != nil {
						logger.ErrorF("Error connecting to remote: %v", err)
						c.cancelForward(flogger, forward)
						return
					}

					if err = protocol.NewMsgExchange(c.cfg.Token, msg.ConnId, forward.Type).Send(nRconn); err != nil {
						logger.LInfoF(flogger, "Error sending exchange msg to remote: %v", err)
					}

					lConn, err := net.DialUDP("udp", nil, &net.UDPAddr{
						IP:   net.ParseIP("0.0.0.0"),
						Port: forward.LocalPort,
					})
					if err != nil {
						logger.LErrorF(flogger, "Error connecting to local: %v, will close forward, %s:%d", err, forward.Type, forward.LocalPort)
						// TODO: need to cancel forward?
						// c.cancelForward(flogger,forward)
						return
					}
					if err := proxy.UDPClientStream(c.cfg.Token, nRconn, lConn); err != nil {
						logger.LErrorF(flogger, "Error proxying udp: %v", err)
						c.cancelForward(flogger, forward)
					}
				}()
			case "tcp":
				go func() {
					logger.LInfoF(flogger, "Receive user req from server, start proxying, conn_id: %s", msg.ConnId)
					nRconn, err := c.newSvrConn()
					if err != nil {
						logger.LErrorF(flogger, "Error connecting to remote: %v", err)
						c.cancelForward(flogger, forward)
						return
					}
					if err = protocol.NewMsgExchange(c.cfg.Token, msg.ConnId, forward.Type).Send(nRconn); err != nil {
						logger.LInfoF(flogger, "Error sending exchange msg to remote: %v", err)
					}
					lConn, err := net.Dial(msg.Type, fmt.Sprintf(":%d", forward.LocalPort))
					if err != nil {
						logger.LErrorF(flogger, "Error connecting to local: %v, will close forward, %s:%d", err, forward.Type, forward.LocalPort)
						// TODO: need to cancel forward?
						// c.cancelForward(flogger,forward)
						return
					}

					if forward.SpeedLimit != "" {
						limit := pio.LimitTransfer(forward.SpeedLimit)
						logger.LDebugF(flogger, "Proxying with limit: %s, transfered limit: %d", forward.SpeedLimit, limit)
						proxy.Stream(pio.NewLimitStream(lConn, limit), nRconn)
					} else {
						proxy.Stream(lConn, nRconn)
					}
				}()
			}
		case protocol.Heartbeat:
			msg := &protocol.MsgHeartbeat{}
			if err := json.Unmarshal(buf, msg); err != nil {
				logger.LErrorF(flogger, "Error read heartbeat msg from remote: %v", err)
				c.cancelForward(flogger, forward)
				return
			}
			if !c.sameToken(msg.Token) {
				logger.LErrorF(flogger, "Receive heartbeat from server, token not match")
				c.cancelForward(flogger, forward)
				return
			}
			logger.LDebug(flogger, "Receive heartbeat from server")
		}
	}
}

func (c *Client) cancelForward(flogger *logger.Logger, forward Forward) {
	rConn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.cfg.SvrHost, c.cfg.SvrPort))
	if err != nil {
		logger.LFatalF(flogger, "Error connecting to remote: %v", err)
	}
	if err = protocol.NewMsgCancel(c.cfg.Token, forward.ProxyName, forward.RemotePort).Send(rConn); err != nil {
		logger.LFatalF(flogger, "Error sending cancel msg to remote: %v", err)
	}
	logger.LInfoF(flogger, "Close connection to server, local port: %d, remote port: %d", forward.LocalPort, forward.RemotePort)
}

func (c *Client) signalShutdown() {
	logger.InfoF("Press Ctrl+C to shutdown")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	<-sc
	logger.InfoF("Receive signal to shutdown")

	for _, forward := range c.cfg.Forwards {
		c.cancelForward(logger.DefaultLogger, forward)
	}

	os.Exit(1)
}

func (c *Client) sameToken(token string) bool {
	return c.cfg.Token == "" || c.cfg.Token == token
}
