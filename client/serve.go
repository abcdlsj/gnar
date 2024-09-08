package client

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/abcdlsj/gnar/client/control"
	"github.com/abcdlsj/gnar/client/tunnel"
	"github.com/abcdlsj/gnar/logger"
	"github.com/abcdlsj/gnar/proto"
)

type Client struct {
	cfg Config
}

type Proxyer struct {
	remotePort int
	localPort  int
	token      string
	svraddr    string // server host:port
	proxyName  string
	subdomain  string
	speedLimit string
	proxyType  string
	ctrlDialer control.AuthSvrDialer
	logger     *logger.Logger

	mu sync.Mutex
}

func newClient(cfg Config) *Client {
	return &Client{
		cfg: cfg,
	}
}

func newProxyer(svraddr string, token string, mux bool, f Proxy) *Proxyer {
	logPrefix := fmt.Sprintf("%s [%d:%d]", strings.ToUpper(f.ProxyType), f.LocalPort, f.RemotePort)
	if f.ProxyName != "" {
		logPrefix = fmt.Sprintf("%s [%s]", strings.ToUpper(f.ProxyType), f.ProxyName)
	}

	proxyer := &Proxyer{
		token:      token,
		svraddr:    svraddr,
		proxyName:  f.ProxyName,
		subdomain:  f.Subdomain,
		remotePort: f.RemotePort,
		localPort:  f.LocalPort,
		speedLimit: f.SpeedLimit,
		proxyType:  f.ProxyType,
		logger:     logger.New(logPrefix),
		ctrlDialer: control.NewTCPDialer(svraddr, token),
	}

	if mux {
		proxyer.ctrlDialer = control.NewMuxDialer(svraddr, token)
	}

	return proxyer
}

func (f *Proxyer) cancel() {
	f.mu.Lock()
	defer f.mu.Unlock()

	conn, err := f.ctrlDialer.Open()
	if err != nil {
		logger.Fatalf("Error connecting to remote: %v", err)
	}
	if err = proto.Send(conn, proto.NewMsgCancel(f.token, f.proxyName, f.remotePort)); err != nil {
		logger.Fatalf("Error sending cancel msg to remote: %v", err)
	}

	logger.Infof("Close connection to server, local port: %d, remote port: %d", f.localPort, f.remotePort)
}

func (c *Client) Run() error {
	logger.Info("Press Ctrl+C to shutdown")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	cancelFns := make([]func(), 0)
	for _, proxy := range c.cfg.Proxys {
		proxyer := newProxyer(c.cfg.SvrAddr, c.cfg.Token, c.cfg.Multiplex, proxy)
		go proxyer.Run()

		cancelFns = append(cancelFns, func() {
			proxyer.cancel()
		})
	}

	logger.Infof("Receive signal %s to shutdown", <-sc)

	for _, cancelFn := range cancelFns {
		cancelFn()
	}

	logger.Info("Shutdown success")
	return nil
}

func (f *Proxyer) Run() {
	defer func() {
		if r := recover(); r != nil {
			f.logger.Fatalf("Proxy panic: %v", r)
		}
	}()

	rConn, err := f.ctrlDialer.Open()
	if err != nil {
		f.logger.Fatalf("Error open svr connection to remote: %v", err)
	}

	f.mustNewProxy(rConn)

	for {
		p, buf, err := proto.Read(rConn)
		if err != nil {
			f.logger.Errorf("Error reading msg from remote: %v", err)
			f.cancel()
			return
		}

		nlogger := f.logger.CloneAdd(p.String())
		switch p {
		case proto.PacketExchange:
			msg := &proto.MsgExchange{}
			if err := json.Unmarshal(buf, msg); err != nil {
				nlogger.Errorf("Error reading exchange msg from remote: %v", err)
				f.cancel()
				return
			}

			f.handleExchange(msg, nlogger)
		case proto.PacketHeartbeat:
			msg := &proto.MsgHeartbeat{}
			if err := json.Unmarshal(buf, msg); err != nil {
				nlogger.Errorf("Error reading heartbeat msg from remote: %v", err)
				f.cancel()
				return
			}

			nlogger.Debug("")
		}
	}
}

func (f *Proxyer) handleExchange(msg *proto.MsgExchange, nlogger *logger.Logger) {
	nlogger.Infof("Receive user conn from server, start proxying, conn_id: %s", msg.ConnId)
	rConn, err := f.ctrlDialer.Open()
	if err != nil {
		nlogger.Errorf("Error connecting to remote: %v", err)
		return
	}

	if err = proto.Send(rConn, proto.NewMsgExchange(msg.ConnId, f.proxyType)); err != nil {
		nlogger.Infof("Error sending exchange msg to remote: %v", err)
		return
	}

	go tunnel.RunTunnel(f.localPort, msg.ProxyType, f.speedLimit, nlogger, rConn)
}

func (f *Proxyer) mustNewProxy(rConn net.Conn) {
	if err := proto.Send(rConn, proto.NewMsgProxy(f.proxyName, f.subdomain,
		f.proxyType, f.remotePort)); err != nil {
		f.logger.Fatalf("Error send proxy msg to remote: %v", err)
	}

	pxyResp := &proto.MsgProxyResp{}
	if err := proto.Recv(rConn, pxyResp); err != nil {
		f.logger.Fatal("Error reading proxy resp msg from remote, please check your config")
	}

	if pxyResp.Status != "success" {
		f.logger.Fatalf("Proxy create failed, status: %s, remote port: %d", pxyResp.Status, f.remotePort)
	}

	if pxyResp.Domain != "" {
		f.logger.Infof("Proxy create success, domain: %s", pxyResp.Domain)
	} else {
		f.logger.Info("Proxy create success!")
	}
}
