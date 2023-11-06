package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/abcdlsj/pipe/logger"
	"github.com/abcdlsj/pipe/pio"
	"github.com/abcdlsj/pipe/proto"
	"github.com/abcdlsj/pipe/proxy"
	"github.com/hashicorp/yamux"
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
	session    *yamux.Session

	mu sync.Mutex
}

func newClient(cfg Config) *Client {
	return &Client{
		cfg: cfg,
	}
}

func newForwarder(svraddr string, token string, multiple bool, f Forward) *Forwarder {
	logPrefix := fmt.Sprintf("%s[%d:%d]", strings.ToUpper(f.ProxyType), f.LocalPort, f.RemotePort)
	if f.ProxyName != "" {
		logPrefix = fmt.Sprintf("%s[%s]", strings.ToUpper(f.ProxyType), f.ProxyName)
	}

	forwarder := &Forwarder{
		token:      token,
		svraddr:    svraddr,
		proxyName:  f.ProxyName,
		subdomain:  f.Subdomain,
		remotePort: f.RemotePort,
		localPort:  f.LocalPort,
		speedLimit: f.SpeedLimit,
		proxyType:  f.ProxyType,
		logger:     logger.New(logPrefix),
		session:    nil,
	}

	if multiple {
		var err error
		forwarder.session, err = authNewSession(svraddr, token)
		if err != nil {
			forwarder.logger.Fatalf("Error connecting to remote: %v", err)
		}
	}

	return forwarder
}

func (f *Forwarder) cancel() {
	f.mu.Lock()
	defer f.mu.Unlock()

	var stream io.Writer
	var err error

	if f.session != nil {
		stream, err = f.session.OpenStream()
		if err != nil {
			f.logger.Warnf("Error opening stream: %v", err)
		}
		logger.Debugf("Open stream to server, local port: %d, remote port: %d", f.localPort, f.remotePort)
	} else {
		stream, err = authDialSvr(f.svraddr, f.token)
		if err != nil {
			f.logger.Fatalf("Error connecting to remote: %v", err)
		}
	}
	if err = proto.Send(stream, proto.NewMsgCancel(f.token, f.proxyName, f.remotePort)); err != nil {
		logger.Fatalf("Error sending cancel msg to remote: %v", err)
	}
	logger.Infof("Close connection to server, local port: %d, remote port: %d", f.localPort, f.remotePort)
}

func (c *Client) Run() {
	logger.Info("Press Ctrl+C to shutdown")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	cancelFns := make([]func(), 0)
	for _, forward := range c.cfg.Forwards {
		forwarder := newForwarder(c.cfg.SvrAddr, c.cfg.Token, c.cfg.Multiple, forward)
		go forwarder.Run()

		cancelFns = append(cancelFns, func() {
			forwarder.cancel()
		})
	}

	logger.Infof("Receive signal %s to shutdown", <-sc)

	for _, cancelFn := range cancelFns {
		cancelFn()
	}

	logger.Info("Shutdown success")
}

func authNewSession(svraddr string, token string) (*yamux.Session, error) {
	conn, err := net.Dial("tcp", svraddr)
	if err != nil {
		return nil, err
	}
	if err = proto.Send(conn, proto.NewMsgLogin(token)); err != nil {
		return nil, err
	}

	session, err := yamux.Client(conn, nil)
	if err != nil {
		return nil, err
	}

	return session, nil
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

func (f *Forwarder) newSvrConn() (net.Conn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.session != nil {
		return f.session.OpenStream()
	}
	return authDialSvr(f.svraddr, f.token)
}

func (f *Forwarder) Run() {
	rConn, err := f.newSvrConn()
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

			switch msg.ProxyType {
			case "udp":
				go func() {
					nlogger.Infof("Receive udp conn from server, start proxying, conn_id: %s", msg.ConnId)
					nRconn, err := f.newSvrConn()
					if err != nil {
						nlogger.Errorf("Error connecting to remote: %v", err)
						return
					}

					if err = proto.Send(nRconn, proto.NewMsgExchange(msg.ConnId, f.proxyType)); err != nil {
						nlogger.Infof("Error sending exchange msg to remote: %v", err)
						return
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
						return
					}
				}()
			case "tcp":
				go func() {
					nlogger.Infof("Receive user req from server, start proxying, conn_id: %s", msg.ConnId)
					nRconn, err := f.newSvrConn()
					if err != nil {
						nlogger.Errorf("Error connecting to remote: %v", err)
						return
					}
					if err = proto.Send(nRconn, proto.NewMsgExchange(msg.ConnId, f.proxyType)); err != nil {
						nlogger.Infof("Error sending exchange msg to remote: %v", err)
						return
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
				f.cancel()
				return
			}

			nlogger.Debug("")
		}
	}
}
