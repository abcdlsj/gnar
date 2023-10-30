package server

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/abcdlsj/pipe/logger"
	"github.com/abcdlsj/pipe/proto"
	"github.com/abcdlsj/pipe/proxy"
	"github.com/abcdlsj/pipe/server/conn"
	"github.com/abcdlsj/pipe/share"
	"github.com/google/uuid"
)

type Server struct {
	cfg         Config
	tcpConnMap  conn.TCPConnMap
	udpConnMap  conn.UDPConnMap
	portManager map[int]bool
	forwards    []Forward

	m sync.RWMutex
}

func newServer(cfg Config) *Server {
	return &Server{
		cfg:         cfg,
		tcpConnMap:  conn.NewTCPConnMap(),
		udpConnMap:  conn.NewUDPConnMap(),
		portManager: make(map[int]bool),
	}
}

func (s *Server) Run() {
	if s.cfg.AdminPort != 0 {
		go s.startAdmin()
	}

	go s.tcpConnMap.StartAutoExpire()

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.Port))
	if err != nil {
		logger.Fatalf("Error listening: %v", err)
	}
	defer listener.Close()

	logger.Infof("Server listening on port %d", s.cfg.Port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			logger.Infof("Error accepting: %v", err)
			return
		}

		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	loginMsg := proto.MsgLogin{}
	if err := proto.Recv(conn, &loginMsg); err != nil {
		logger.Errorf("Error reading from connection: %v", err)
		conn.Close()
		return
	}

	hash := md5.New()
	hash.Write([]byte(s.cfg.Token + fmt.Sprintf("%d", loginMsg.Timestamp)))

	if fmt.Sprintf("%x", hash.Sum(nil)) != loginMsg.Token {
		logger.Errorf("Invalid token, client addr: %s", conn.RemoteAddr().String())
		conn.Close()
		return
	}

	if share.GetVersion() != loginMsg.Version {
		logger.Warnf("Client version not match, client addr: %s", conn.RemoteAddr().String())
	}

	logger.Debugf("Auth success, client addr: %s", conn.RemoteAddr().String())

	pt, buf, err := proto.Read(conn)
	if err != nil {
		logger.Errorf("Error reading from connection: %v", err)
		return
	}

	switch pt {
	case proto.PacketForwardReq:
		failChan := make(chan struct{})
		defer close(failChan)

		go func() {
			<-failChan
			if err := proto.Send(conn, proto.NewMsgForwardResp("", "failed")); err != nil {
				logger.Errorf("Error sending forward failed resp message: %v", err)
			}
		}()

		msg := &proto.MsgForwardReq{}
		if err := json.Unmarshal(buf, msg); err != nil {
			logger.Errorf("Error unmarshalling message: %v", err)
			return
		}

		s.handleForward(conn, msg, failChan)
	case proto.PacketExchange:
		msg := &proto.MsgExchange{}
		if err := json.Unmarshal(buf, msg); err != nil {
			logger.Errorf("Error unmarshalling message: %v", err)
			return
		}

		s.handleExchange(conn, msg)
	case proto.PacketForwardCancel:
		defer conn.Close()
		msg := &proto.MsgForwardCancel{}
		if err := json.Unmarshal(buf, msg); err != nil {
			logger.Errorf("Error unmarshalling message: %v", err)
			return
		}

		s.handleCancel(msg)
	}
}

func (s *Server) handleCancel(msg *proto.MsgForwardCancel) {
	s.delForward(msg.RemotePort)
	logger.Infof("Forward port %d canceled", msg.RemotePort)
}

func (s *Server) handleForward(cConn net.Conn, msg *proto.MsgForwardReq, failChan chan struct{}) {
	uPort := msg.RemotePort
	if !s.availablePort(uPort) {
		logger.Errorf("Invalid forward to port: %d", uPort)
		failChan <- struct{}{}
		return
	}

	from := cConn.RemoteAddr().String()

	switch msg.ProxyType {
	case "udp":
		udpListener, err := net.ListenUDP("udp", &net.UDPAddr{
			IP:   net.ParseIP("0.0.0.0"),
			Port: uPort,
		})
		if err != nil {
			logger.Errorf("Error listening: %v, port: %d, type: %s", err, uPort, msg.ProxyType)
			failChan <- struct{}{}
			return
		}
		logger.Infof("Listening on forwarding port %d, type: %s", uPort, msg.ProxyType)
		s.addForward(Forward{
			To:           uPort,
			From:         from,
			Subdomain:    msg.Subdomain,
			listenCloser: udpListener,
		})

		logger.Infof("Receive forward from %s to port %d", from, uPort)
		logger.Infof("Send forward accept msg to client: %s", from)
		domain := fmt.Sprintf("%s.%s", msg.Subdomain, s.cfg.Domain)
		if !s.cfg.DomainTunnel {
			domain = ""
		}

		if err = proto.Send(cConn, proto.NewMsgForwardResp(domain, "success")); err != nil {
			logger.Errorf("Error sending forward accept message: %v", err)
			failChan <- struct{}{}
			return
		}

		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()

			for range ticker.C {
				if err := proto.Send(cConn, proto.NewMsgHeartbeat()); err != nil {
					logger.Warnf("Error sending heartbeat message: %v, forward to port: %d", err, uPort)
					return
				}
			}
		}()

		uid := conn.NewUuid()
		s.udpConnMap.Add(uid, udpListener)
		if err := proto.Send(cConn, proto.NewMsgExchange(uid, msg.ProxyType)); err != nil {
			logger.Errorf("Error sending exchange message: %v", err)
		}
		logger.Debugf("Send udp listener to client, id: %s", uid)
	case "tcp":
		uListener, err := net.Listen("tcp", fmt.Sprintf(":%d", uPort))
		if err != nil {
			logger.Errorf("Error listening: %v, port: %d, type: %s", err, uPort, msg.ProxyType)
			failChan <- struct{}{}
			return
		}
		defer uListener.Close()

		logger.Infof("Listening on forwarding port %d, type: %s", uPort, msg.ProxyType)
		s.addForward(Forward{
			To:           uPort,
			From:         from,
			Subdomain:    msg.Subdomain,
			listenCloser: uListener,
		})

		logger.Infof("Receive forward from %s to port %d", from, uPort)
		logger.Infof("Send forward accept msg to client: %s", from)

		domain := fmt.Sprintf("%s.%s", msg.Subdomain, s.cfg.Domain)
		if !s.cfg.DomainTunnel {
			domain = ""
		}

		if err = proto.Send(cConn, proto.NewMsgForwardResp(domain, "success")); err != nil {
			logger.Errorf("Error sending forward accept message: %v", err)
			failChan <- struct{}{}
			return
		}

		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()

			for range ticker.C {
				if err := proto.Send(cConn, proto.NewMsgHeartbeat()); err != nil {
					logger.Warnf("Error sending heartbeat message: %v, forward to port: %d", err, uPort)
					return
				}
			}
		}()

		for {
			userConn, err := uListener.Accept()
			if err != nil {
				return
			}
			logger.Debugf("Receive new user conn: %s", userConn.RemoteAddr().String())
			go func() {
				uid := conn.NewUuid()
				s.tcpConnMap.Add(uid, userConn)
				if err := proto.Send(cConn, proto.NewMsgExchange(uid, msg.ProxyType)); err != nil {
					logger.Errorf("Error sending exchange message: %v", err)
				}
				logger.Debugf("Send new user conn id: %s", uid)
			}()
		}
	}
}

func (s *Server) handleExchange(conn net.Conn, msg *proto.MsgExchange) {
	switch msg.ProxyType {
	case "udp":
		logger.Debugf("Receive udp conn exchange msg from client: %s", msg.ConnId)
		uConn, ok := s.udpConnMap.Get(msg.ConnId)
		if !ok {
			return
		}
		defer s.udpConnMap.Del(msg.ConnId)
		proxy.UDPDatagram(s.cfg.Token, conn, uConn)
	case "tcp":
		logger.Debugf("Receive tcp conn exchange msg from client: %s", msg.ConnId)
		uConn, ok := s.tcpConnMap.Get(msg.ConnId)
		if !ok {
			return
		}

		defer s.tcpConnMap.Del(msg.ConnId)
		proxy.Stream(conn, uConn)
	}
}

func (s *Server) availablePort(port int) bool {
	s.m.Lock()
	defer s.m.Unlock()

	return port > 0 && port < 65535 && !s.portManager[port]
}

type Forward struct {
	To           int
	From         string
	Subdomain    string
	listenCloser io.Closer
}

func (s *Server) addForward(f Forward) {
	s.m.Lock()
	defer s.m.Unlock()

	if s.cfg.DomainTunnel {
		if f.Subdomain == "" {
			f.Subdomain = fmt.Sprintf("%s.%s", uuid.NewString()[:7], s.cfg.Domain)
		} else {
			f.Subdomain = fmt.Sprintf("%s.%s", f.Subdomain, s.cfg.Domain)
		}
		go addCaddyRouter(f.Subdomain, f.To)
	}

	s.forwards = append(s.forwards, f)
	s.portManager[f.To] = true
}

func (s *Server) delForward(to int) {
	s.m.Lock()
	defer s.m.Unlock()
	for i, ff := range s.forwards {
		if ff.To == to {
			ff.listenCloser.Close()
			if s.cfg.DomainTunnel {
				go delCaddyRouter(fmt.Sprintf("%s.%d", ff.Subdomain, ff.To))
			}
			s.forwards = append(s.forwards[:i], s.forwards[i+1:]...)
			logger.Infof("Receive cancel forward from %s to port %d", ff.From, ff.To)
			s.portManager[ff.To] = false
			return
		}
	}
}
