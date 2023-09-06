package server

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"

	"github.com/abcdlsj/pipe/logger"
	"github.com/abcdlsj/pipe/packet"
	"github.com/abcdlsj/pipe/proxy"
	"github.com/google/uuid"
)

type Server struct {
	cfg      Config
	connMap  ConnMap
	forwards []Forward
	traffics []proxy.Traffic

	m sync.RWMutex
}

func newServer(cfg Config) *Server {
	return &Server{
		cfg: cfg,
		connMap: ConnMap{
			conns: make(map[string]net.Conn),
		},
	}
}

func (s *Server) Run() {
	if s.cfg.AdminPort != 0 {
		go s.startAdmin()
	}

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

func (s *Server) checkToken(token string) bool {
	return s.cfg.Token != "" && s.cfg.Token != token
}

func (s *Server) handle(conn net.Conn) {
	pt, buf, err := packet.ReadMsg(conn)
	if err != nil {
		logger.ErrorF("Error reading from connection: %v", err)
		return
	}

	switch pt {
	case packet.Forward:
		failChan := make(chan struct{})
		defer close(failChan)

		go func() {
			defer conn.Close()
			<-failChan
			if err := packet.Accept.Send(conn, s.cfg.Token, "", "failed"); err != nil {
				logger.ErrorF("Error sending accept message: %v", err)
			}
		}()

		msg := &packet.MsgForward{}
		if err := json.Unmarshal(buf, msg); err != nil {
			logger.ErrorF("Error unmarshalling message: %v", err)
			return
		}
		if s.checkToken(msg.Token) {
			logger.WarnF("Receive new forward request, token not match: [%s]", msg.Token)
			failChan <- struct{}{}
			return
		}

		s.handleForward(conn, msg, failChan)
	case packet.Exchange:
		msg := &packet.MsgExchang{}
		if err := json.Unmarshal(buf, msg); err != nil {
			logger.ErrorF("Error unmarshalling message: %v", err)
			return
		}
		if s.checkToken(msg.Token) {
			logger.WarnF("Receive exchange request, token not match: [%s]", msg.Token)
			return
		}
		s.handleExchange(conn, msg)
	case packet.Cancel:
		msg := &packet.MsgCancel{}
		if err := json.Unmarshal(buf, msg); err != nil {
			logger.ErrorF("Error unmarshalling message: %v", err)
			return
		}
		if s.checkToken(msg.Token) {
			logger.WarnF("Receive cancel request, token not match: [%s]", msg.Token)
			return
		}
		s.handleCancel(msg)
	}
}

func (s *Server) handleCancel(msg *packet.MsgCancel) {
	s.delForward(msg.RemotePort)
	logger.InfoF("Cancel port %d forward", msg.RemotePort)
}

func (s *Server) handleForward(commuConn net.Conn, msg *packet.MsgForward, failChan chan struct{}) {
	uPort := msg.RemotePort
	if isInvaliedPort(uPort) {
		logger.ErrorF("Invalid forward to port: %d", uPort)
		failChan <- struct{}{}
		return
	}

	uListener, err := net.Listen("tcp", fmt.Sprintf(":%d", uPort))
	if err != nil {
		logger.ErrorF("Error listening: %v, port: %d", err, uPort)
		failChan <- struct{}{}
		return
	}
	defer uListener.Close()

	logger.InfoF("Listening on forwarding port %d", uPort)
	s.addForward(Forward{
		From:      commuConn.RemoteAddr().String(),
		To:        uPort,
		uListener: uListener,
		SubDomain: msg.SubDomain,
	})

	logger.InfoF("Forward from %s to port %d", commuConn.RemoteAddr().String(), uPort)
	logger.DebugF("Send accept message to client: %s", commuConn.RemoteAddr().String())

	domain := fmt.Sprintf("%s.%s", msg.SubDomain, s.cfg.Domain)
	if !s.cfg.DomainTunnel {
		domain = ""
	}

	if err := packet.Accept.Send(commuConn, s.cfg.Token, domain, "success"); err != nil {
		logger.ErrorF("Error sending accept message: %v", err)
		return
	}

	for {
		userConn, err := uListener.Accept()
		if err != nil {
			return
		}
		logger.DebugF("Accept new user connection: %s", userConn.RemoteAddr().String())
		go func() {
			cid := uuid.NewString()[:packet.Len-1]
			s.addUserConn(cid, userConn)
			if err := packet.Exchange.Send(commuConn, s.cfg.Token, cid); err != nil {
				logger.ErrorF("Error sending message: %v", err)
			}
			logger.DebugF("Send new connection id: %s", cid)
		}()
	}
}

func (s *Server) handleExchange(conn net.Conn, msg *packet.MsgExchang) {
	logger.DebugF("Receive message from client: %s", msg.ConnId)
	uConn, ok := s.getUserConn(msg.ConnId)
	if !ok {
		return
	}

	defer s.delUserConn(msg.ConnId)
	s.metric(proxy.P(conn, uConn))
}

func isInvaliedPort(port int) bool {
	return port < 0 || port > 65535
}

type Forward struct {
	From      string
	To        int
	SubDomain string

	uListener net.Listener
}

func (s *Server) addUserConn(cid string, conn net.Conn) {
	s.connMap.Add(cid, conn)
}

func (s *Server) delUserConn(cid string) {
	s.connMap.Del(cid)
}

func (s *Server) getUserConn(cid string) (net.Conn, bool) {
	return s.connMap.Get(cid)
}

func (s *Server) addForward(f Forward) {
	s.m.Lock()
	defer s.m.Unlock()

	if s.cfg.DomainTunnel {
		if f.SubDomain == "" {
			f.SubDomain = fmt.Sprintf("%s.%s", uuid.NewString()[:7], s.cfg.Domain)
		} else {
			f.SubDomain = fmt.Sprintf("%s.%s", f.SubDomain, s.cfg.Domain)
		}
		go addCaddyRouter(f.SubDomain, f.To)
	}

	s.forwards = append(s.forwards, f)
}

func (s *Server) delForward(to int) {
	s.m.Lock()
	defer s.m.Unlock()
	for i, ff := range s.forwards {
		if ff.To == to {
			ff.uListener.Close()
			if s.cfg.DomainTunnel {
				go delCaddyRouter(fmt.Sprintf("%s.%d", ff.SubDomain, ff.To))
			}
			s.forwards = append(s.forwards[:i], s.forwards[i+1:]...)
			logger.InfoF("Cancel forward from %s to port %d", ff.From, ff.To)
			return
		}
	}
}

func (s *Server) metric(t proxy.Traffic) {
	s.m.Lock()
	defer s.m.Unlock()
	s.traffics = append(s.traffics, t)
}
