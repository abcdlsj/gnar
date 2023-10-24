package client

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/abcdlsj/pipe/logger"
	"github.com/abcdlsj/pipe/pio"
	"github.com/abcdlsj/pipe/proto"
	"github.com/abcdlsj/pipe/proxy"
)

type Client struct {
	cfg Config
}

type Forwarder struct {
	remotePort int
	localPort  int
	token      string
	svraddr    string // server host:port
	proxyName  string
	subdomain  string
	speedLimit string
	proxyType  string
	logger     *logger.Logger
}

func newClient(cfg Config) *Client {
	return &Client{
		cfg: cfg,
	}
}

func newForwarder(svraddr string, token string, f Forward) *Forwarder {
	logPrefix := fmt.Sprintf("%s[%d:%d]", strings.ToUpper(f.ProxyType), f.LocalPort, f.RemotePort)
	if f.ProxyName != "" {
		logPrefix = fmt.Sprintf("%s[%s]", strings.ToUpper(f.ProxyType), f.ProxyName)
	}

	return &Forwarder{
		token:      token,
		svraddr:    svraddr,
		proxyName:  f.ProxyName,
		subdomain:  f.Subdomain,
		remotePort: f.RemotePort,
		localPort:  f.LocalPort,
		speedLimit: f.SpeedLimit,
		proxyType:  f.ProxyType,
		logger:     logger.New(logPrefix),
	}
}

func (c *Client) Run() {
	logger.Info("Press Ctrl+C to shutdown")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	for _, forward := range c.cfg.Forwards {
		go newForwarder(c.cfg.SvrAddr, c.cfg.Token, forward).Run()
	}

	<-sc
	logger.Info("Receive signal to shutdown")
	for _, f := range c.cfg.Forwards {
		cancelForward(c.cfg.Token, c.cfg.SvrAddr, f.ProxyName, f.LocalPort, f.RemotePort)
	}
}

func authDialSvr(svraddr string, token string) (net.Conn, error) {
	conn, err := net.Dial("tcp", svraddr)
	if err != nil {
		return nil, err
	}

	if err = proto.Send(conn, proto.NewMsgLogin(token)); err != nil {
		return nil, err
	}

	return conn, nil
}

func (f *Forwarder) Run() {
	rConn, err := authDialSvr(f.svraddr, f.token)
	if err != nil {
		f.logger.Fatalf("Error connecting to remote: %v", err)
	}

	if err = proto.Send(rConn, proto.NewMsgForward(f.proxyName, f.subdomain,
		f.proxyType, f.remotePort)); err != nil {

		f.logger.Fatalf("Error send forward msg to remote: %v", err)
	}

	frdResp := &proto.MsgForwardResp{}
	if err = proto.Recv(rConn, frdResp); err != nil {
		f.logger.Fatal("Error reading forward resp msg from remote, please check your config")
	}

	if frdResp.Status != "success" {
		f.logger.Fatalf("Forward failed, status: %s, remote port: %d", frdResp.Status, f.remotePort)
	}

	if frdResp.Domain != "" {
		f.logger.Infof("Forward success, remote port: %d, domain: %s", f.remotePort, frdResp.Domain)
	} else {
		f.logger.Infof("Forward success, remote port: %d", f.remotePort)
	}

	for {
		p, buf, err := proto.Read(rConn)
		if err != nil {
			f.logger.Errorf("Error reading msg from remote: %v", err)
			return
		}

		nlogger := f.logger.CloneAdd(p.String())
		switch p {
		case proto.PacketExchange:
			msg := &proto.MsgExchange{}
			if err := json.Unmarshal(buf, msg); err != nil {
				nlogger.Errorf("Error reading exchange msg from remote: %v", err)
				cancelForward(f.token, f.svraddr, f.proxyName, f.localPort, f.remotePort)
				return
			}

			switch msg.ProxyType {
			case "udp":
				go func() {
					nlogger.Infof("Receive udp conn from server, start proxying, conn_id: %s", msg.ConnId)
					nRconn, err := authDialSvr(f.svraddr, f.token)
					if err != nil {
						nlogger.Errorf("Error connecting to remote: %v", err)
						cancelForward(f.token, f.svraddr, f.proxyName, f.localPort, f.remotePort)
						return
					}

					if err = proto.Send(nRconn, proto.NewMsgExchange(msg.ConnId, f.proxyType)); err != nil {
						nlogger.Infof("Error sending exchange msg to remote: %v", err)
					}

					lConn, err := net.DialUDP("udp", nil, &net.UDPAddr{
						IP:   net.ParseIP("0.0.0.0"),
						Port: f.localPort,
					})

					if err != nil {
						nlogger.Errorf("Error connecting to local: %v, will close forward, %s:%d", err, f.proxyType, f.localPort)
						return
					}
					if err := proxy.UDPClientStream(f.token, nRconn, lConn); err != nil {
						nlogger.Errorf("Error proxying udp: %v", err)
						cancelForward(f.token, f.svraddr, f.proxyName, f.localPort, f.remotePort)
					}
				}()
			case "tcp":
				go func() {
					nlogger.Infof("Receive user req from server, start proxying, conn_id: %s", msg.ConnId)
					nRconn, err := authDialSvr(f.svraddr, f.token)
					if err != nil {
						nlogger.Errorf("Error connecting to remote: %v", err)
						cancelForward(f.token, f.svraddr, f.proxyName, f.localPort, f.remotePort)
						return
					}
					if err = proto.Send(nRconn, proto.NewMsgExchange(msg.ConnId, f.proxyType)); err != nil {
						nlogger.Infof("Error sending exchange msg to remote: %v", err)
					}
					lConn, err := net.Dial(msg.ProxyType, fmt.Sprintf(":%d", f.localPort))
					if err != nil {
						nlogger.Errorf("Error connecting to local: %v, will close forward, %s:%d", err, f.proxyType, f.localPort)
						return
					}

					if f.speedLimit != "" {
						limit := pio.LimitTransfer(f.speedLimit)
						nlogger.Debugf("Proxying with limit: %s, transfered limit: %d", f.speedLimit, limit)
						proxy.Stream(pio.NewLimitStream(lConn, limit), nRconn)
					} else {
						proxy.Stream(lConn, nRconn)
					}
				}()
			}
		case proto.PacketHeartbeat:
			msg := &proto.MsgHeartbeat{}
			if err := json.Unmarshal(buf, msg); err != nil {
				nlogger.Errorf("Error reading heartbeat msg from remote: %v", err)
				cancelForward(f.token, f.svraddr, f.proxyName, f.localPort, f.remotePort)
				return
			}

			nlogger.Debug("")
		}
	}
}

func cancelForward(token, svr, proxyName string, lport, rport int) {
	rConn, err := authDialSvr(svr, token)
	if err != nil {
		logger.Fatalf("Error connecting to remote: %v", err)
	}
	if err = proto.Send(rConn, proto.NewMsgCancel(token, proxyName, rport)); err != nil {
		logger.Fatalf("Error sending cancel msg to remote: %v", err)
	}
	logger.Infof("Close connection to server, local port: %d, remote port: %d", lport, rport)
}
