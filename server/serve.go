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
	authenticator auth.Authenticator
	resources     *resourceManager
}

type resourceManager struct {
	proxys        []Proxy
	portManager   map[int]bool
	domainManager map[string]bool
	caddySrvName  string
	m             sync.RWMutex
}

func newResourceManager(cfg Config) *resourceManager {
	return &resourceManager{
		proxys:        []Proxy{},
		portManager:   make(map[int]bool),
		domainManager: make(map[string]bool),
		caddySrvName:  cfg.CaddySrvName,
	}
}

func newServer(cfg Config) *Server {
	s := &Server{
		cfg:           cfg,
		tcpConnMap:    conn.NewTCPConnMap(),
		udpConnMap:    conn.NewUDPConnMap(),
		authenticator: &auth.Nop{},
		resources:     newResourceManager(cfg),
	}

	if s.cfg.Token != "" {
		s.authenticator = auth.NewTokenAuthenticator(s.cfg.Token)
	}

	return s
}

func (s *Server) Run() error {
	s.printMetaInfo()
	s.startAdminServer()
	s.startProxyServer()
	return nil
}

func (s *Server) printMetaInfo() {
	fmt.Println("---")
	fmt.Println("Gnar Server")
	fmt.Printf("Version: %s\n", share.GetVersion())
	fmt.Printf("Port: %d\n", s.cfg.Port)
	fmt.Printf("Admin Port: %d\n", s.cfg.AdminPort)
	fmt.Printf("Domain Tunnel: %v\n", s.cfg.DomainTunnel)
	fmt.Printf("Domain: %s\n", s.cfg.Domain)
	fmt.Printf("Token: %s\n", s.cfg.Token)
	fmt.Printf("Token Authentication: %v\n", s.cfg.Token != "")
	fmt.Printf("Multiplex: %v\n", s.cfg.Multiplex)
	fmt.Printf("Caddy Server Name: %s\n", s.cfg.CaddySrvName)
	fmt.Println("---")
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
	s.resources.removeProxy(msg.RemotePort)
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
	if !s.resources.isAvailablePort(uPort) {
		failCh <- struct{}{}
		return fmt.Errorf("invalid proxy to port: %d", uPort)
	}

	domain, err := s.resources.distrDomain(msg.Subdomain, s.cfg, uPort)
	if err != nil {
		failCh <- struct{}{}
		return err
	}

	proxyHandler, err := s.createProxyHandler(msg.ProxyType, uPort)
	if err != nil {
		failCh <- struct{}{}
		return err
	}

	err = s.setupAndRunProxy(proxyHandler, uPort, domain, cConn, msg)
	if err != nil {
		failCh <- struct{}{}
		return err
	}

	return nil
}

func (rm *resourceManager) distrDomain(sub string, cfg Config, uPort int) (string, error) {
	rm.m.Lock()
	defer rm.m.Unlock()

	if !cfg.DomainTunnel {
		return "", nil
	}

	if sub == "" {
		sub = uuid.NewString()[:8]
	}

	domain := fmt.Sprintf("%s.%s", sub, cfg.Domain)

	if !rm.domainManager[domain] {
		if err := addCaddyRouter(rm.caddySrvName, domain, uPort); err != nil {
			return "", err
		}
		return domain, nil
	}

	return domain, errors.New("domain already used")
}

type proxyHandler interface {
	listen() (interface{}, error)
	handleConn(s *Server, listener interface{}, cConn net.Conn, msg *proto.MsgProxyReq) error
}

type tcpProxyHandler struct {
	uPort int
}

func (h *tcpProxyHandler) listen() (interface{}, error) {
	return net.Listen("tcp", fmt.Sprintf(":%d", h.uPort))
}

func (h *tcpProxyHandler) handleConn(s *Server, listener interface{}, cConn net.Conn, msg *proto.MsgProxyReq) error {
	tcpListener := listener.(net.Listener)
	for {
		userConn, err := tcpListener.Accept()
		if err != nil {
			return fmt.Errorf("error accepting: %v", err)
		}
		go s.handleTCPUserConn(userConn, cConn, msg)
	}
}

type udpProxyHandler struct {
	uPort int
}

func (h *udpProxyHandler) listen() (interface{}, error) {
	return net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("0.0.0.0"), Port: h.uPort})
}

func (h *udpProxyHandler) handleConn(s *Server, conn interface{}, cConn net.Conn, msg *proto.MsgProxyReq) error {
	udpConn := conn.(*net.UDPConn)
	uid := uuid.New().String()
	s.udpConnMap.Add(uid, udpConn)
	if err := proto.Send(cConn, proto.NewMsgExchange(uid, msg.ProxyType)); err != nil {
		return fmt.Errorf("error sending exchange message: %v", err)
	}
	return nil
}

func (s *Server) createProxyHandler(proxyType string, uPort int) (proxyHandler, error) {
	switch proxyType {
	case "tcp":
		return &tcpProxyHandler{uPort}, nil
	case "udp":
		return &udpProxyHandler{uPort}, nil
	default:
		return nil, fmt.Errorf("invalid proxy type: %s", proxyType)
	}
}

func (s *Server) setupAndRunProxy(handler proxyHandler, uPort int, domain string, cConn net.Conn, msg *proto.MsgProxyReq) error {
	listener, err := handler.listen()
	if err != nil {
		return fmt.Errorf("error listening: %v", err)
	}

	from := cConn.RemoteAddr().String()
	s.resources.addProxy(Proxy{
		Port:   uPort,
		From:   from,
		Domain: domain,
		Closer: listener.(io.Closer),
	})

	logger.Infof("Listening on proxying port %d, type: %s", uPort, msg.ProxyType)
	logger.Infof("Receive proxy from %s to port %d", from, uPort)
	logger.Infof("Send proxy accept msg to client: %s", from)

	if err = proto.Send(cConn, proto.NewMsgProxyResp(domain, "success")); err != nil {
		return fmt.Errorf("error sending proxy accept message: %v", err)
	}

	go tickHeart(cConn, logger.New(fmt.Sprintf("[:%d]", uPort)))

	return handler.handleConn(s, listener, cConn, msg)
}

func (s *Server) handleTCPUserConn(userConn net.Conn, cConn net.Conn, msg *proto.MsgProxyReq) {
	uid := conn.NewUuid()
	s.tcpConnMap.Add(uid, userConn)
	if err := proto.Send(cConn, proto.NewMsgExchange(uid, msg.ProxyType)); err != nil {
		logger.Errorf("Error sending exchange message: %v", err)
	}
	logger.Debugf("Send new user conn id: %s", uid)
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

func (rm *resourceManager) isAvailablePort(port int) bool {
	rm.m.RLock()
	defer rm.m.RUnlock()
	return port > 0 && port < 65535 && !rm.portManager[port]
}

func (rm *resourceManager) addProxy(f Proxy) {
	rm.m.Lock()
	defer rm.m.Unlock()

	rm.proxys = append(rm.proxys, f)
	rm.portManager[f.Port] = true
	rm.domainManager[f.Domain] = true
}

func (rm *resourceManager) removeProxy(port int) {
	rm.m.Lock()
	defer rm.m.Unlock()
	for i, proxy := range rm.proxys {
		if proxy.Port == port {
			proxy.Closer.Close()
			if rm.domainManager[proxy.Domain] {
				delCaddyRouter(fmt.Sprintf("%s.%d", proxy.Domain, proxy.Port))
			}
			rm.proxys = append(rm.proxys[:i], rm.proxys[i+1:]...)
			delete(rm.portManager, proxy.Port)
			delete(rm.domainManager, proxy.Domain)
			return
		}
	}
}

type Proxy struct {
	Port   int
	From   string
	Domain string
	Closer io.Closer
}
