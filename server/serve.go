package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/abcdlsj/pipe/logger"
	"github.com/abcdlsj/pipe/protocol"
	"github.com/abcdlsj/pipe/proxy"
	"github.com/google/uuid"
)

type Server struct {
	cfg         Config
	tcpConnMap  TCPConnMap
	udpConnMap  UDPConnMap
	forwards    []Forward
	traffics    []proxy.Traffic
	portManager map[int]bool

	m sync.RWMutex
}

func newServer(cfg Config) *Server {
	return &Server{
		cfg: cfg,
		tcpConnMap: TCPConnMap{
			conns: make(map[string]TCPConn),
		},
		udpConnMap: UDPConnMap{
			conns: make(map[string]*net.UDPConn),
		},
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
		logger.FatalF("Error listening: %v", err)
	}
	defer listener.Close()

	logger.InfoF("Server listen on port %d", s.cfg.Port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			logger.InfoF("Error accepting: %v", err)
			return
		}

		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	pt, buf, err := protocol.Read(conn)
	if err != nil {
		logger.ErrorF("Error reading from connection: %v", err)
		return
	}

	switch pt {
	case protocol.Forward:
		failChan := make(chan struct{})
		defer close(failChan)

		go func() {
			<-failChan
			if err := protocol.NewMsgForwardResp(s.cfg.Token, "", "failed").Send(conn); err != nil {
				logger.ErrorF("Error sending forward failed resp message: %v", err)
			}
		}()

		msg := &protocol.MsgForward{}
		if err := json.Unmarshal(buf, msg); err != nil {
			logger.ErrorF("Error unmarshalling message: %v", err)
			return
		}
		if !s.sameToken(msg.Token) {
			logger.WarnF("Receive new forward request, token not match: [%s]", msg.Token)
			failChan <- struct{}{}
			return
		}

		s.handleForward(conn, msg, failChan)
	case protocol.Exchange:
		msg := &protocol.MsgExchange{}
		if err := json.Unmarshal(buf, msg); err != nil {
			logger.ErrorF("Error unmarshalling message: %v", err)
			return
		}
		if !s.sameToken(msg.Token) {
			logger.WarnF("Receive exchange request, token not match: [%s]", msg.Token)
			return
		}
		s.handleExchange(conn, msg)
	case protocol.Cancel:
		defer conn.Close()
		msg := &protocol.MsgCancel{}
		if err := json.Unmarshal(buf, msg); err != nil {
			logger.ErrorF("Error unmarshalling message: %v", err)
			return
		}
		if !s.sameToken(msg.Token) {
			logger.WarnF("Receive cancel request, token not match: [%s]", msg.Token)
			return
		}
		s.handleCancel(msg)
	}
}

func (s *Server) handleCancel(msg *protocol.MsgCancel) {
	s.delForward(msg.RemotePort)
	logger.InfoF("Forward port %d canceled", msg.RemotePort)
}

func (s *Server) handleForward(cConn net.Conn, msg *protocol.MsgForward, failChan chan struct{}) {
	uPort := msg.RemotePort
	if !s.availablePort(uPort) {
		logger.ErrorF("Invalid forward to port: %d", uPort)
		failChan <- struct{}{}
		return
	}

	from := cConn.RemoteAddr().String()

	switch msg.Type {
	case "udp":
		udpListener, err := net.ListenUDP("udp", &net.UDPAddr{
			IP:   net.ParseIP("0.0.0.0"),
			Port: uPort,
		})
		if err != nil {
			logger.ErrorF("Error listening: %v, port: %d, type: %s", err, uPort, msg.Type)
			failChan <- struct{}{}
			return
		}
		logger.InfoF("Listening on forwarding port %d, type: %s", uPort, msg.Type)
		s.addForward(Forward{
			To:           uPort,
			From:         from,
			Subdomain:    msg.Subdomain,
			listenCloser: udpListener,
		})
		logger.InfoF("Receive forward from %s to port %d", from, uPort)
		logger.InfoF("Send forward accept msg to client: %s", from)
		domain := fmt.Sprintf("%s.%s", msg.Subdomain, s.cfg.Domain)
		if !s.cfg.DomainTunnel {
			domain = ""
		}

		if err = protocol.NewMsgForwardResp(s.cfg.Token, domain, "success").Send(cConn); err != nil {
			logger.ErrorF("Error sending forward accept message: %v", err)
			failChan <- struct{}{}
			return
		}

		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()

			for range ticker.C {
				if err := protocol.NewMsgHeartbeat(s.cfg.Token).Send(cConn); err != nil {
					logger.WarnF("Error sending heartbeat message: %v, forward to port: %d", err, uPort)
					return
				}
			}
		}()

		cid := uuid.NewString()[:ConnUUIDLen]
		s.udpConnMap.Add(cid, udpListener)
		if err := protocol.NewMsgExchange(s.cfg.Token, cid, msg.Type).Send(cConn); err != nil {
			logger.ErrorF("Error sending exchange message: %v", err)
		}
		logger.DebugF("Send udp listener to client, id: %s", cid)
	case "tcp":
		uListener, err := net.Listen("tcp", fmt.Sprintf(":%d", uPort))
		if err != nil {
			logger.ErrorF("Error listening: %v, port: %d, type: %s", err, uPort, msg.Type)
			failChan <- struct{}{}
			return
		}
		defer uListener.Close()

		logger.InfoF("Listening on forwarding port %d, type: %s", uPort, msg.Type)
		s.addForward(Forward{
			To:           uPort,
			From:         from,
			Subdomain:    msg.Subdomain,
			listenCloser: uListener,
		})

		logger.InfoF("Receive forward from %s to port %d", from, uPort)
		logger.InfoF("Send forward accept msg to client: %s", from)

		domain := fmt.Sprintf("%s.%s", msg.Subdomain, s.cfg.Domain)
		if !s.cfg.DomainTunnel {
			domain = ""
		}

		if err = protocol.NewMsgForwardResp(s.cfg.Token, domain, "success").Send(cConn); err != nil {
			logger.ErrorF("Error sending forward accept message: %v", err)
			failChan <- struct{}{}
			return
		}

		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()

			for range ticker.C {
				if err := protocol.NewMsgHeartbeat(s.cfg.Token).Send(cConn); err != nil {
					logger.WarnF("Error sending heartbeat message: %v, forward to port: %d", err, uPort)
					return
				}
			}
		}()

		for {
			userConn, err := uListener.Accept()
			if err != nil {
				return
			}
			logger.DebugF("Receive new user connection: %s", userConn.RemoteAddr().String())
			go func() {
				cid := uuid.NewString()[:ConnUUIDLen]
				s.tcpConnMap.Add(cid, userConn)
				if err := protocol.NewMsgExchange(s.cfg.Token, cid, msg.Type).Send(cConn); err != nil {
					logger.ErrorF("Error sending exchange message: %v", err)
				}
				logger.DebugF("Send new user connection id: %s", cid)
			}()
		}
	}
}

func (s *Server) handleExchange(conn net.Conn, msg *protocol.MsgExchange) {
	switch msg.Type {
	case "udp":
		logger.DebugF("Receive udp conn exchange msg from client: %s", msg.ConnId)
		uConn, ok := s.udpConnMap.Get(msg.ConnId)
		if !ok {
			return
		}
		defer s.udpConnMap.Del(msg.ConnId)
		proxy.ProxyUDP(s.cfg.Token, conn, uConn)
	case "tcp":
		logger.DebugF("Receive tcp conn exchange msg from client: %s", msg.ConnId)
		uConn, ok := s.tcpConnMap.Get(msg.ConnId)
		if !ok {
			return
		}

		defer s.tcpConnMap.Del(msg.ConnId)
		s.metric(proxy.P(conn, uConn))
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
			logger.InfoF("Receive cancel forward from %s to port %d", ff.From, ff.To)
			s.portManager[ff.To] = false
			return
		}
	}
}

func (s *Server) metric(t proxy.Traffic) {
	s.m.Lock()
	defer s.m.Unlock()
	s.traffics = append(s.traffics, t)
}

func (s *Server) sameToken(token string) bool {
	return s.cfg.Token == "" || s.cfg.Token == token
}
