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

	"github.com/abcdlsj/gnar/internal/client/control"
	"github.com/abcdlsj/gnar/internal/logger"
	"github.com/abcdlsj/gnar/internal/ui"
	"github.com/abcdlsj/gnar/pkg/proto"
	"github.com/abcdlsj/gnar/pkg/share"
)

type Client struct {
	cfg Config
}

type Proxyer struct {
	remotePort int
	localPort  int
	token      string
	svraddr    string
	proxyName  string
	subdomain  string
	speedLimit string
	proxyType  string
	ctrlDialer control.AuthSvrDialer
	logPrefix  string
	mu         sync.Mutex
}

func newClient(cfg Config) *Client {
	return &Client{cfg: cfg}
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
		logPrefix:  logPrefix,
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
	c.printMetaInfo()
	if len(c.cfg.Proxys) == 0 {
		logger.Error("No proxy config found, please check your config")
		return nil
	}
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
	logger.Info("Press Ctrl+C to shutdown")
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
			logger.Fatalf("%s Proxy panic: %v", f.logPrefix, r)
		}
	}()

	rConn, err := f.ctrlDialer.Open()
	if err != nil {
		logger.Fatalf("%s Error open svr connection to remote: %v", f.logPrefix, err)
	}

	f.mustNewProxy(rConn)

	for {
		p, buf, err := proto.Read(rConn)
		if err != nil {
			logger.Errorf("%s Error reading msg from remote: %v", f.logPrefix, err)
			f.cancel()
			return
		}

		switch p {
		case proto.PacketExchange:
			msg := &proto.MsgExchange{}
			if err := json.Unmarshal(buf, msg); err != nil {
				logger.Errorf("%s Error reading exchange msg from remote: %v", f.logPrefix, err)
				f.cancel()
				return
			}
			f.handleExchange(msg)
		case proto.PacketHeartbeat:
			logger.Debugf("%s Heartbeat", f.logPrefix)
		}
	}
}

func (f *Proxyer) handleExchange(msg *proto.MsgExchange) {
	logger.Infof("%s Receive user conn, start proxying, conn_id: %s", f.logPrefix, msg.ConnId)
	rConn, err := f.ctrlDialer.Open()
	if err != nil {
		logger.Errorf("%s Error connecting to remote: %v", f.logPrefix, err)
		return
	}

	if err = proto.Send(rConn, proto.NewMsgExchange(msg.ConnId, f.proxyType)); err != nil {
		logger.Errorf("%s Error sending exchange msg to remote: %v", f.logPrefix, err)
		return
	}

	go runTunnel(f.localPort, msg.ProxyType, f.speedLimit, rConn)
}

func (f *Proxyer) mustNewProxy(rConn net.Conn) {
	if err := proto.Send(rConn, proto.NewMsgProxy(f.proxyName, f.subdomain,
		f.proxyType, f.remotePort)); err != nil {
		logger.Fatalf("%s Error send proxy msg to remote: %v", f.logPrefix, err)
	}

	pxyResp := &proto.MsgProxyResp{}
	if err := proto.Recv(rConn, pxyResp); err != nil {
		logger.Fatal("Error reading proxy resp msg from remote, please check your config")
	}

	if pxyResp.Status != "success" {
		logger.Fatalf("%s Proxy create failed, status: %s, remote port: %d", f.logPrefix, pxyResp.Status, f.remotePort)
	}

	if pxyResp.Domain != "" {
		logger.Infof("%s Proxy create success, domain: https://%s", f.logPrefix, pxyResp.Domain)
	} else {
		logger.Infof("%s Proxy create success!", f.logPrefix)
	}
}

func (c *Client) printMetaInfo() {
	fmt.Println(ui.RenderClientBanner(ui.ClientInfo{
		Version:   share.GetVersion(),
		SvrAddr:   c.cfg.SvrAddr,
		Multiplex: c.cfg.Multiplex,
		Token:     c.cfg.Token != "",
	}))

	var proxies []ui.ProxyInfo
	for _, p := range c.cfg.Proxys {
		proxies = append(proxies, ui.ProxyInfo{
			Name:       p.ProxyName,
			LocalPort:  p.LocalPort,
			RemotePort: p.RemotePort,
			ProxyType:  p.ProxyType,
			Subdomain:  p.Subdomain,
			SpeedLimit: p.SpeedLimit,
		})
	}
	if len(proxies) > 0 {
		fmt.Println(ui.RenderProxyList(proxies))
	}
}
