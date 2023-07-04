package server

import (
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/abcdlsj/gpipe/layer"
	"github.com/abcdlsj/gpipe/logger"
	"github.com/abcdlsj/gpipe/proxy"
	"github.com/google/uuid"
)

type Config struct {
	Port         int    `toml:"port"`
	AdminPort    int    `toml:"admin-port"`    // zero means disable admin server
	DomainTunnel bool   `toml:"domain-tunnel"` // enable domain tunnel
	Domain       string `toml:"domain"`        // domain name
}

type Server struct {
	cfg      Config
	connMap  ConnMap
	forwards []Forward
	traffics []proxy.Traffic

	m sync.RWMutex
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
		f.SubDomain = fmt.Sprintf("%s.%s", uuid.NewString()[:7], s.cfg.Domain)
		go addCaddyRouter(f.SubDomain, f.To)
	}

	s.forwards = append(s.forwards, f)
	logger.InfoF("Forward from %s to port %d", f.From, f.To)
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

func newServer(cfg Config) *Server {
	return &Server{
		cfg: cfg,
		connMap: ConnMap{
			conns: make(map[string]net.Conn),
		},
	}
}

func parseConfig(cfgFile string) Config {
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		logger.FatalF("Error reading config file: %v", err)
	}

	var cfg Config
	toml.Unmarshal(data, &cfg)

	return cfg
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

func (s *Server) handle(conn net.Conn) {
	packetType, buf, err := layer.Read(conn)
	if err != nil || buf == nil {
		logger.WarnF("Error reading from connection: %v", err)
		return
	}

	switch packetType {
	case layer.RegisterForward:
		s.handleForward(conn, buf)
	case layer.ExchangeMsg:
		s.handleMessage(conn, buf)
	case layer.CancelForward:
		s.handleCancel(layer.ParseCancelPacket(buf))
	}
}

func (s *Server) handleCancel(rPort int) {
	s.delForward(rPort)
	logger.InfoF("Cancel forward to port %d", rPort)
}

func (s *Server) handleForward(commuConn net.Conn, buf []byte) {
	uPort := layer.ParseRegisterPacket(buf)
	if isInvaliedPort(uPort) {
		logger.ErrorF("Invalid forward to port: %d", uPort)
		return
	}

	uListener, err := net.Listen("tcp", fmt.Sprintf(":%d", uPort))
	if err != nil {
		logger.ErrorF("Error listening: %v, port: %d", err, uPort)
		return
	}
	defer uListener.Close()

	logger.InfoF("Listening on forwarding port %d", uPort)
	s.addForward(Forward{
		From:      commuConn.RemoteAddr().String(),
		To:        uPort,
		uListener: uListener,
	})
	for {
		userConn, err := uListener.Accept()
		if err != nil {
			return
		}
		logger.DebugF("Accept new user connection: %s", userConn.RemoteAddr().String())
		go func() {
			cid := uuid.NewString()[:layer.Len-1]
			s.addUserConn(cid, userConn)
			layer.ExchangeMsg.Send(commuConn, cid)
		}()
	}
}

func (s *Server) handleMessage(conn net.Conn, buf []byte) {
	rid := layer.ParseExchangePacket(buf)
	uConn, ok := s.getUserConn(rid)
	if !ok {
		return
	}

	defer s.delUserConn(rid)
	s.metric(proxy.P(conn, uConn))
}

func isInvaliedPort(port int) bool {
	return port < 0 || port > 65535
}
