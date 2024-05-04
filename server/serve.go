package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/abcdlsj/gnar/auth"
	"github.com/abcdlsj/gnar/logger"
	"github.com/abcdlsj/gnar/proto"
	"github.com/abcdlsj/gnar/proxy"
	"github.com/abcdlsj/gnar/server/conn"
	"github.com/abcdlsj/gnar/share"
	"github.com/google/uuid"
	"github.com/hashicorp/yamux"
)

type Server struct {
	cfg           Config
	tcpConnMap    conn.TCPConnMap
	udpConnMap    conn.UDPConnMap
	portManager   map[int]bool
	domainManager map[string]bool
	authenticator auth.Authenticator
	proxys        []Proxy

	m sync.RWMutex
}

func newServer(cfg Config) *Server {
	s := &Server{
		cfg:           cfg,
		tcpConnMap:    conn.NewTCPConnMap(),
		udpConnMap:    conn.NewUDPConnMap(),
		portManager:   make(map[int]bool),
		domainManager: make(map[string]bool),
		authenticator: &auth.Nop{},
	}

	if s.cfg.Token != "" {
		s.authenticator = auth.NewTokenAuthenticator(s.cfg.Token)
	}

	return s
}

func (s *Server) Run() {
	s.startAdminServer()
	s.startProxyServer()
}

func (s *Server) startAdminServer() {
	if s.cfg.AdminPort != 0 {
		go s.startAdmin()
	}
}

func (s *Server) startProxyServer() {
	go s.tcpConnMap.StartAutoExpire()

	listener := s.createListener()
	defer listener.Close()

	s.acceptConnections(listener)
}

func (s *Server) createListener() net.Listener {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.Port))
	if err != nil {
		logger.Fatalf("Error listening: %v", err)
	}
	logger.Infof("Server listening on port %d", s.cfg.Port)
	return listener
}

func (s *Server) acceptConnections(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			logger.Infof("Error accepting: %v", err)
			return
		}

		s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	if s.cfg.Multiplex {
		s.handleMultiplexConnection(conn)
	} else {
		go s.handle(conn, false)
	}
}

func (s *Server) handleMultiplexConnection(conn net.Conn) {
	go func() {
		session, err := s.newMuxSession(conn)
		if session == nil {
			conn.Close()
			return
		}
		if err != nil {
			logger.Errorf("Error creating yamux session: %v", err)
			return
		}
		s.handleMuxSession(session, conn)
	}()
}

func (s *Server) handleMuxSession(session *yamux.Session, conn net.Conn) {
	for {
		stream, err := session.AcceptStream()
		if err != nil {
			logger.Errorf("Error accepting stream: %v", err)
			return
		}
		logger.Debugf("New yamux connection, client addr: %s", conn.RemoteAddr().String())

		go s.handle(stream, true)
	}
}

func (s *Server) newMuxSession(conn net.Conn) (*yamux.Session, error) {
	if err := s.authCheckConn(conn); err != nil {
		return nil, err
	}

	session, err := yamux.Server(conn, nil)
	if err != nil {
		logger.Errorf("Error creating yamux session: %v", err)
		conn.Close()
		return nil, err
	}

	return session, nil
}

func (s *Server) handle(conn net.Conn, mux bool) {
	if !mux {
		if err := s.authCheckConn(conn); err != nil {
			logger.Errorf("Authentication failed: %v", err)
			conn.Close()
			return
		}
	}

	for {
		pt, buf, err := proto.Read(conn)
		if err != nil {
			logger.Errorf("Error reading packet: %v", err)
			return
		}

		if err := s.handlePacket(conn, pt, buf); err != nil {
			logger.Errorf("Error handling packet: %v", err)
			return
		}
	}
}

func (s *Server) handlePacket(conn net.Conn, pt proto.PacketType, buf []byte) error {
	switch pt {
	case proto.PacketProxyReq:
		return s.handleProxyReq(conn, buf)
	case proto.PacketExchange:
		return s.handleExchange(conn, buf)
	case proto.PacketProxyCancel:
		return s.handleProxyCancel(conn, buf)
	default:
		return fmt.Errorf("unknown packet type: %v", pt)
	}
}

func (s *Server) handleProxyReq(conn net.Conn, buf []byte) error {
	msg := &proto.MsgProxyReq{}
	if err := json.Unmarshal(buf, msg); err != nil {
		return fmt.Errorf("error unmarshalling proxy request: %v", err)
	}

	failCh := make(chan struct{})
	go s.sendFailureResponse(conn, failCh)

	err := s.handleProxy(conn, msg, failCh)
	if err != nil {
		logger.Errorf("Error handling proxy: %v", err)
		close(failCh)
	}
	return err
}

func (s *Server) sendFailureResponse(conn net.Conn, failCh <-chan struct{}) {
	select {
	case <-failCh:
		if err := proto.Send(conn, proto.NewMsgProxyResp("", "failed")); err != nil {
			logger.Errorf("Error sending proxy failed resp message: %v", err)
		}
	case <-time.After(10 * time.Second):
		// TODO: when timeout, do what?
	}
}

func (s *Server) handleExchange(conn net.Conn, buf []byte) error {
	msg := &proto.MsgExchange{}
	if err := json.Unmarshal(buf, msg); err != nil {
		return fmt.Errorf("error unmarshalling exchange message: %v", err)
	}

	return s.handleExchangeMsg(conn, msg)
}

func (s *Server) handleProxyCancel(conn net.Conn, buf []byte) error {
	msg := &proto.NewProxyCancel{}
	if err := json.Unmarshal(buf, msg); err != nil {
		return fmt.Errorf("error unmarshalling proxy cancel message: %v", err)
	}

	defer conn.Close()
	s.removeProxy(msg.RemotePort)
	logger.Infof("Proxy port %d canceled", msg.RemotePort)
	return nil
}

func (s *Server) authCheckConn(conn net.Conn) error {
	loginMsg := proto.MsgLogin{}
	if err := proto.Recv(conn, &loginMsg); err != nil {
		logger.Errorf("Error reading from connection: %v", err)
		return err
	}

	if ok := s.authenticator.VerifyLogin(&loginMsg); !ok {
		logger.Errorf("Invalid token, client addr: %s", conn.RemoteAddr().String())
		return fmt.Errorf("invalid token")
	}

	if share.GetVersion() != loginMsg.Version {
		logger.Warnf("Client version not match, client addr: %s", conn.RemoteAddr().String())
	}

	logger.Debugf("Auth success, client addr: %s", conn.RemoteAddr().String())
	return nil
}

func (s *Server) handleProxy(cConn net.Conn, msg *proto.MsgProxyReq, failCh chan struct{}) error {
	uPort := msg.RemotePort
	if !s.isVailablePort(uPort) {
		failCh <- struct{}{}
		return fmt.Errorf("invalid proxy to port: %d", uPort)
	}

	var domain string
	var err error
	if s.cfg.DomainTunnel {
		domain, err = s.distrDomain(msg.Subdomain)
		if err != nil {
			failCh <- struct{}{}
			return fmt.Errorf("invalid subdomain: %s", msg.Subdomain)
		}

		if err := addCaddyRouter(domain, uPort); err != nil {
			failCh <- struct{}{}
			return fmt.Errorf("error adding caddy router: %v", err)
		}
	}

	switch msg.ProxyType {
	case "tcp":
		err = s.handleTCPProxy(uPort, domain, cConn, msg)
	case "udp":
		err = s.handleUDPProxy(uPort, domain, cConn, msg)
	default:
		failCh <- struct{}{}
		return fmt.Errorf("invalid proxy type: %s", msg.ProxyType)
	}

	if err != nil {
		failCh <- struct{}{}
		return err
	}

	return nil
}

func (s *Server) handleUDPProxy(uPort int, domain string, cConn net.Conn, msg *proto.MsgProxyReq) error {
	from := cConn.RemoteAddr().String()

	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   net.ParseIP("0.0.0.0"),
		Port: uPort,
	})
	if err != nil {
		logger.Errorf("Error listening: %v, port: %d, type: %s", err, uPort, msg.ProxyType)
		return fmt.Errorf("error listening: %v", err)
	}
	// defer udpConn.Close() // don't close

	logger.Infof("Listening on proxying port %d, type: %s", uPort, msg.ProxyType)

	if s.cfg.DomainTunnel {
		domain, err = s.distrDomain(msg.Subdomain)
		if err != nil {
			logger.Errorf("Invalid subdomain: %s, err: %v", msg.Subdomain, err)
			return fmt.Errorf("invalid subdomain: %s", msg.Subdomain)
		}
	}

	s.flushProxy(Proxy{
		To:      uPort,
		From:    from,
		Domain:  domain,
		uCloser: udpConn,
	})

	logger.Infof("Receive proxy from %s to port %d", from, uPort)
	logger.Infof("Send proxy accept msg to client: %s", from)

	if err = proto.Send(cConn, proto.NewMsgProxyResp(domain, "success")); err != nil {
		logger.Errorf("Error sending proxy accept message: %v", err)
		return fmt.Errorf("error sending proxy accept message: %v", err)
	}

	go tickHeart(cConn, logger.New(fmt.Sprintf("[:%d]", uPort)))

	uid := conn.NewUuid()
	s.udpConnMap.Add(uid, udpConn)
	if err := proto.Send(cConn, proto.NewMsgExchange(uid, msg.ProxyType)); err != nil {
		logger.Errorf("Error sending exchange message: %v", err)
	}

	logger.Debugf("Send udp listener to client, id: %s", uid)
	return nil
}

func (s *Server) handleTCPProxy(uPort int, domain string, cConn net.Conn, msg *proto.MsgProxyReq) error {
	from := cConn.RemoteAddr().String()

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", uPort))
	if err != nil {
		logger.Errorf("Error listening: %v, port: %d, type: %s", err, uPort, msg.ProxyType)
		return fmt.Errorf("error listening: %v", err)
	}
	defer listener.Close()

	logger.Infof("Listening on proxying port %d, type: %s", uPort, msg.ProxyType)

	if s.cfg.DomainTunnel {
		domain, err = s.distrDomain(msg.Subdomain)
		if err != nil {
			logger.Errorf("Invalid subdomain: %s, err: %v", msg.Subdomain, err)
			return fmt.Errorf("invalid subdomain: %s", msg.Subdomain)
		}
	}

	s.flushProxy(Proxy{
		To:      uPort,
		From:    from,
		Domain:  domain,
		uCloser: listener,
	})

	logger.Infof("Receive proxy from %s to port %d", from, uPort)
	logger.Infof("Send proxy accept msg to client: %s", from)

	if err = proto.Send(cConn, proto.NewMsgProxyResp(domain, "success")); err != nil {
		logger.Errorf("Error sending proxy accept message: %v", err)
		return fmt.Errorf("error sending proxy accept message: %v", err)
	}

	go tickHeart(cConn, logger.New(fmt.Sprintf("[:%d]", uPort)))

	for {
		userConn, err := listener.Accept()
		if err != nil {
			return fmt.Errorf("error accepting: %v", err)
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

func tickHeart(cConn net.Conn, hlogger *logger.Logger) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if err := proto.Send(cConn, proto.NewMsgHeartbeat()); err != nil {
			hlogger.Warnf("Error sending heartbeat message: %v", err)
			return
		}
	}
}

func (s *Server) handleExchangeMsg(conn net.Conn, msg *proto.MsgExchange) error {
	switch msg.ProxyType {
	case "udp":
		logger.Debugf("Receive udp conn exchange msg from client: %s", msg.ConnId)
		uConn, ok := s.udpConnMap.Get(msg.ConnId)
		if !ok {
			return fmt.Errorf("udp connection not found: %s", msg.ConnId)
		}
		defer s.udpConnMap.Del(msg.ConnId)
		proxy.UDPDatagram(conn, uConn)
	case "tcp":
		logger.Debugf("Receive tcp conn exchange msg from client: %s", msg.ConnId)
		uConn, ok := s.tcpConnMap.Get(msg.ConnId)
		if !ok {
			return fmt.Errorf("tcp connection not found: %s", msg.ConnId)
		}

		defer s.tcpConnMap.Del(msg.ConnId)
		proxy.Stream(conn, uConn)
	default:
		return fmt.Errorf("invalid proxy type: %s", msg.ProxyType)
	}

	return nil
}

func (s *Server) isVailablePort(port int) bool {
	s.m.Lock()
	defer s.m.Unlock()

	return port > 0 && port < 65535 && !s.portManager[port]
}
func (s *Server) distrDomain(sub string) (string, error) {
	s.m.Lock()
	defer s.m.Unlock()

	if !s.cfg.DomainTunnel {
		return "", nil
	}

	if sub == "" {
		sub = uuid.NewString()[:10]
	}

	domain := fmt.Sprintf("%s.%s", sub, s.cfg.Domain)

	if !s.domainManager[domain] {
		return domain, nil
	}

	return domain, errors.New("domain already used")
}

type Proxy struct {
	To      int
	From    string
	Domain  string
	uCloser io.Closer
}

func (s *Server) flushProxy(f Proxy) {
	s.m.Lock()
	defer s.m.Unlock()

	s.proxys = append(s.proxys, f)
	s.portManager[f.To] = true
	s.domainManager[f.Domain] = true
}

func (s *Server) removeProxy(to int) {
	s.m.Lock()
	defer s.m.Unlock()
	for i, ff := range s.proxys {
		if ff.To == to {
			ff.uCloser.Close()
			if s.cfg.DomainTunnel {
				delCaddyRouter(fmt.Sprintf("%s.%d", ff.Domain, ff.To))
			}
			s.proxys = append(s.proxys[:i], s.proxys[i+1:]...)
			s.portManager[ff.To] = false
			s.domainManager[ff.Domain] = false
			return
		}
	}
}
