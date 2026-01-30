# gnar v2 技术设计文档

## 1. 设计目标

- **极致易用**: 一键暴露本地服务，零配置上手
- **优雅架构**: 库与 UI 分离，可复用、可扩展
- **现代传输**: 基于 QUIC，内建 TLS 1.3，自动证书管理
- **OAuth 认证**: 类 OAuth2 流程，refresh token 本地持久化

## 2. 总体架构

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              应用层 (Application)                            │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   ┌──────────────┐    ┌──────────────┐    ┌──────────────┐                 │
│   │   CLI 命令   │    │   TUI 界面   │    │   配置文件   │                 │
│   │  (cobra)     │    │(charmbracelet)│   │  (viper)     │                 │
│   └──────┬───────┘    └──────┬───────┘    └──────┬───────┘                 │
│          │                   │                   │                         │
│          └───────────────────┴───────────────────┘                         │
│                              │                                             │
│                              ▼                                             │
│   ┌─────────────────────────────────────────────────────────────────────┐ │
│   │                     pkg/tunnel (可复用库)                            │ │
│   │  ┌───────────────────────────────────────────────────────────────┐  │ │
│   │  │                    Client API                                 │  │ │
│   │  │  • Auth(ctx, token)                                           │  │ │
│   │  │  • Connect(ctx)                                               │  │ │
│   │  │  • Expose(ctx, port, opts) -> *Tunnel                         │  │ │
│   │  │  • Tunnels() []*Tunnel                                        │  │ │
│   │  │  • Close()                                                    │  │ │
│   │  └───────────────────────────────────────────────────────────────┘  │ │
│   │                                                                     │ │
│   │  ┌───────────────────────────────────────────────────────────────┐  │ │
│   │  │                    Server API                                 │  │ │
│   │  │  • NewServer(cfg)                                             │  │ │
│   │  │  • Run(ctx)                                                   │  │ │
│   │  │  • Register(handler)                                          │  │ │
│   │  └───────────────────────────────────────────────────────────────┘  │ │
│   │                                                                     │ │
│   │  ┌───────────────────────────────────────────────────────────────┐  │ │
│   │  │                  Event System                                 │  │ │
│   │  │  • ConnectionStateChanged                                     │  │ │
│   │  │  • TunnelEstablished / TunnelClosed                           │  │ │
│   │  │  • AuthTokenRefreshed                                         │  │ │
│   │  │  • TrafficStats                                               │  │ │
│   │  └───────────────────────────────────────────────────────────────┘  │ │
│   └─────────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────┘
                                     │
                                     │ QUIC Protocol
                                     ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Server 端                                      │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   ┌──────────────┐    ┌──────────────┐    ┌──────────────┐                 │
│   │  Auth API    │    │  Tunnel Mgr  │    │  HTTPS Router│                 │
│   │  /auth/token │    │  (QUIC)      │    │  (autocert)  │                 │
│   └──────────────┘    └──────────────┘    └──────────────┘                 │
│                                                                             │
│   ┌──────────────┐    ┌──────────────┐    ┌──────────────┐                 │
│   │ Domain Mgr   │    │  Port Mgr    │    │  Stats Mgr   │                 │
│   │ (防占用检测) │    │  (自动分配)  │    │  (流量统计)  │                 │
│   └──────────────┘    └──────────────┘    └──────────────┘                 │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 3. 模块设计

### 3.1 pkg/tunnel (核心库)

```go
package tunnel

// ==================== 客户端 ====================

type ClientConfig struct {
    ServerAddr   string        // server 地址
    QUIC         QUICConfig    // QUIC 配置
    AuthStore    AuthStore     // 认证存储接口
}

type QUICConfig struct {
    TLSCert      string        // TLS 证书路径 (可选，默认自签名)
    TLSKey       string        // TLS 密钥路径 (可选)
    Port         int           // 本地绑定端口 (可选，默认 0=随机)
    IdleTimeout  time.Duration // 连接空闲超时
    HandshakeTimeout time.Duration // 握手超时
}

type Client struct {
    config    ClientConfig
    conn      quic.Connection
    auth      *authManager
    tunnels   map[string]*Tunnel
    events    *eventEmitter
    mu        sync.RWMutex
}

// 认证管理
func (c *Client) Auth(ctx context.Context, token string) error
func (c *Client) IsAuthenticated() bool

// 连接管理
func (c *Client) Connect(ctx context.Context) error
func (c *Client) Disconnect() error
func (c *Client) ConnectionState() ConnectionState

// Tunnel 管理
func (c *Client) Expose(ctx context.Context, localPort int, opts ExposeOptions) (*Tunnel, error)
func (c *Client) CloseTunnel(tunnelID string) error
func (c *Client) Tunnels() []*Tunnel

// 事件订阅
func (c *Client) OnEvent(eventType EventType, handler EventHandler)
func (c *Client) OffEvent(eventType EventType, handler EventHandler)

// ==================== 服务端 ====================

type ServerConfig struct {
    ListenAddr   string        // 监听地址
    QUIC         QUICConfig    // QUIC 配置
    HTTPS        HTTPSConfig   // HTTPS 配置
    Domain       DomainConfig  // 域名配置
}

type HTTPSConfig struct {
    Enabled      bool
    AutoCert     bool          // 自动 Let's Encrypt
    CertDir      string        // 证书缓存目录
}

type DomainConfig struct {
    BaseDomain   string        // 基础域名，如 gnar.example.com
    RandomLen    int           // 随机子域名长度，默认 8
}

type Server struct {
    config    ServerConfig
    listener  *quic.Listener
    auth      *authHandler
    tunnels   *tunnelManager
    domains   *domainManager
    events    *eventEmitter
}

func NewServer(cfg ServerConfig) (*Server, error)
func (s *Server) Run(ctx context.Context) error
func (s *Server) Shutdown(ctx context.Context) error

// ==================== Tunnel 对象 ====================

type Tunnel struct {
    ID           string        // 唯一标识
    LocalPort    int           // 本地端口
    PublicURL    string        // 公网访问地址
    ServerPort   int           // 服务端分配端口
    Status       TunnelStatus  // 状态
    CreatedAt    time.Time
    Stats        *TunnelStats
    
    client       *Client
    stream       quic.Stream
    mu           sync.RWMutex
}

func (t *Tunnel) Close() error
func (t *Tunnel) Status() TunnelStatus
func (t *Tunnel) Stats() TunnelStats

// ==================== 事件系统 ====================

type EventType string

const (
    EventConnectionStateChanged EventType = "connection_state_changed"
    EventTunnelEstablished      EventType = "tunnel_established"
    EventTunnelClosed           EventType = "tunnel_closed"
    EventTunnelError            EventType = "tunnel_error"
    EventAuthTokenRefreshed     EventType = "auth_token_refreshed"
    EventTrafficStats           EventType = "traffic_stats"
)

type Event interface {
    Type() EventType
    Timestamp() time.Time
}

type ConnectionStateChangedEvent struct {
    OldState ConnectionState
    NewState ConnectionState
    Error    error
}

type TunnelEstablishedEvent struct {
    Tunnel *Tunnel
}

type TrafficStatsEvent struct {
    TunnelID    string
    BytesSent   int64
    BytesRecv   int64
    Connections int
}

type EventHandler func(Event)
type eventEmitter struct {
    handlers map[EventType][]EventHandler
    mu       sync.RWMutex
}

func (e *eventEmitter) Emit(event Event)
func (e *eventEmitter) On(eventType EventType, handler EventHandler)
func (e *eventEmitter) Off(eventType EventType, handler EventHandler)
```

### 3.2 认证系统

```go
// OAuth2 风格认证

type AuthStore interface {
    Save(cred *Credential) error
    Load(server string) (*Credential, error)
    Delete(server string) error
    List() ([]*Credential, error)
}

type Credential struct {
    Server       string    // server 地址
    RefreshToken string    // 长期 token (存储在 keyring)
    AccessToken  string    // 短期 token (内存缓存)
    ExpiresAt    time.Time // access_token 过期时间
    Default      bool      // 是否为默认 server
}

type authManager struct {
    store      AuthStore
    credential *Credential
    mu         sync.RWMutex
}

// 自动刷新机制
func (a *authManager) EnsureValidToken(ctx context.Context) (string, error) {
    // 1. 检查 access_token 是否有效
    // 2. 如果即将过期（< 5分钟），自动刷新
    // 3. 如果 refresh_token 过期，返回错误要求重新 auth
}

// Server 端认证 API
type AuthHandler struct {
    tokens     map[string]*TokenInfo // refresh_token -> user
    accessTTL  time.Duration         // access_token 有效期，默认 1h
    refreshTTL time.Duration         // refresh_token 有效期，默认 1year
}

func (h *AuthHandler) HandleTokenRequest(w http.ResponseWriter, r *http.Request) {
    // POST /api/v1/auth/token
    // Request: { "token": "user_provided_token" }
    // Response: { "refresh_token": "xxx", "expires_in": 31536000 }
}

func (h *AuthHandler) HandleRefreshRequest(w http.ResponseWriter, r *http.Request) {
    // POST /api/v1/auth/refresh
    // Request: { "refresh_token": "xxx" }
    // Response: { "access_token": "yyy", "expires_in": 3600 }
}
```

### 3.3 协议设计

```go
// QUIC 连接建立后，使用 bidirectional stream 通信

// ==================== 认证阶段 ====================
// Stream 1: 认证

const PacketTypeAuth byte = 0x01
const PacketTypeAuthResp byte = 0x02

type AuthPacket struct {
    Type        byte   `json:"type"`
    AccessToken string `json:"access_token"`
    Version     string `json:"version"`
}

type AuthResponse struct {
    Type    byte   `json:"type"`
    Success bool   `json:"success"`
    Error   string `json:"error,omitempty"`
}

// ==================== Tunnel 建立 ====================
// Stream 2~N: 每个 Tunnel 独占一个 Stream

const PacketTypeTunnelReq byte = 0x10
const PacketTypeTunnelResp byte = 0x11
const PacketTypeData byte = 0x20
const PacketTypeHeartbeat byte = 0x30

type TunnelRequest struct {
    Type      byte   `json:"type"`
    ReqID     string `json:"req_id"`      // 请求 ID，用于匹配响应
    LocalPort int    `json:"local_port"`
    Subdomain string `json:"subdomain,omitempty"` // 自定义子域名
    Protocol  string `json:"protocol"`    // http, https
}

type TunnelResponse struct {
    Type      byte   `json:"type"`
    ReqID     string `json:"req_id"`
    Success   bool   `json:"success"`
    TunnelID  string `json:"tunnel_id,omitempty"`
    PublicURL string `json:"public_url,omitempty"`  // https://xxx.gnar.dev
    ServerPort int   `json:"server_port,omitempty"`
    Error     string `json:"error,omitempty"`
}

// Data 包直接在 Stream 上传输 HTTP 流量
// 格式: [4字节长度][数据]

// Heartbeat 保活
const PacketTypeHeartbeatReq byte = 0x30
const PacketTypeHeartbeatResp byte = 0x31
```

### 3.4 Server 端管理

```go
// ==================== Domain Manager ====================

type DomainManager struct {
    baseDomain   string
    randomLen    int
    usedDomains  map[string]*DomainInfo // domain -> info
    mu           sync.RWMutex
}

type DomainInfo struct {
    Domain    string
    TunnelID  string
    CreatedAt time.Time
}

func (m *DomainManager) Allocate(subdomain string, tunnelID string) (string, error) {
    // 1. 如果 subdomain 为空，生成随机子域名
    // 2. 检查是否已被占用 (本地 map)
    // 3. 分配并记录
    // 返回完整域名: subdomain.baseDomain
}

func (m *DomainManager) Release(domain string)
func (m *DomainManager) IsAvailable(domain string) bool

// ==================== Port Manager ====================

type PortManager struct {
    startPort int
    endPort   int
    usedPorts map[int]string // port -> tunnelID
    mu        sync.RWMutex
}

func (m *PortManager) Allocate(tunnelID string) (int, error) {
    // 自动分配可用端口
}

func (m *PortManager) Release(port int)

// ==================== Tunnel Manager ====================

type TunnelManager struct {
    tunnels map[string]*ServerTunnel
    mu      sync.RWMutex
}

type ServerTunnel struct {
    ID        string
    ClientID  string
    LocalAddr string       // 客户端声明的本地地址
    PublicURL string
    ServerPort int
    Domain    string
    Status    TunnelStatus
    Stream    quic.Stream
    CreatedAt time.Time
}

func (m *TunnelManager) Register(tunnel *ServerTunnel) error
func (m *TunnelManager) Unregister(tunnelID string)
func (m *TunnelManager) Get(tunnelID string) *ServerTunnel
func (m *TunnelManager) ListByClient(clientID string) []*ServerTunnel
```

### 3.5 HTTPS 路由

```go
type HTTPSRouter struct {
    autocert   *autocert.Manager
    domains    *DomainManager
    tunnels    *TunnelManager
}

func (r *HTTPSRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
    // 1. 从 Host 头获取域名
    // 2. 查找域名对应的 Tunnel
    // 3. 将请求转发到对应的 QUIC Stream
}

func (r *HTTPSRouter) Forward(tunnelID string, stream quic.Stream, req *http.Request) {
    // HTTP 请求序列化 -> Stream 写入 -> 等待响应 -> 返回给客户端
}
```

## 4. 错误设计

```go
package tunnel

import "errors"

// 错误类型分类
type ErrorCode string

const (
    ErrCodeAuthFailed       ErrorCode = "AUTH_FAILED"
    ErrCodeTokenExpired     ErrorCode = "TOKEN_EXPIRED"
    ErrCodeTokenInvalid     ErrorCode = "TOKEN_INVALID"
    ErrCodeDomainTaken      ErrorCode = "DOMAIN_TAKEN"
    ErrCodePortUnavailable  ErrorCode = "PORT_UNAVAILABLE"
    ErrCodeConnectionFailed ErrorCode = "CONNECTION_FAILED"
    ErrCodeTunnelFailed     ErrorCode = "TUNNEL_FAILED"
    ErrCodeServerError      ErrorCode = "SERVER_ERROR"
)

type Error struct {
    Code    ErrorCode
    Message string
    Cause   error
}

func (e *Error) Error() string
func (e *Error) Unwrap() error

// 便捷判断
func IsAuthError(err error) bool
func IsDomainTaken(err error) bool

// 错误实例
var (
    ErrNotAuthenticated = &Error{Code: ErrCodeAuthFailed, Message: "not authenticated"}
    ErrTokenExpired     = &Error{Code: ErrCodeTokenExpired, Message: "token expired, please re-auth"}
    ErrDomainTaken      = &Error{Code: ErrCodeDomainTaken, Message: "domain already in use"}
    ErrConnectionClosed = &Error{Code: ErrCodeConnectionFailed, Message: "connection closed"}
)
```

## 5. TUI 设计

### 5.1 组件规划

```go
package tui

import (
    "github.com/charmbracelet/bubbles/list"
    "github.com/charmbracelet/bubbles/spinner"
    "github.com/charmbracelet/bubbles/table"
    "github.com/charmbracelet/bubbles/textinput"
    "github.com/charmbracelet/bubbletea"
    "github.com/charmbracelet/huh"
    "github.com/charmbracelet/lipgloss"
)

// ==================== 样式定义 ====================

var (
    // 主题色
    primaryColor   = lipgloss.Color("#7D56F4")
    successColor   = lipgloss.Color("#04B575")
    errorColor     = lipgloss.Color("#F25D94")
    warningColor   = lipgloss.Color("#F4D03F")
    infoColor      = lipgloss.Color("#3498DB")
    
    // 样式
    titleStyle     = lipgloss.NewStyle().Bold(true).Foreground(primaryColor)
    successStyle   = lipgloss.NewStyle().Foreground(successColor)
    errorStyle     = lipgloss.NewStyle().Foreground(errorColor)
    urlStyle       = lipgloss.NewStyle().Bold(true).Foreground(infoColor).Underline(true)
)

// ==================== 核心组件 ====================

// AuthForm - Token 输入表单
type AuthForm struct {
    form *huh.Form
}

func NewAuthForm(server string) *AuthForm {
    return &AuthForm{
        form: huh.NewForm(
            huh.NewGroup(
                huh.NewNote().
                    Title("🔐 认证").
                    Description(fmt.Sprintf("登录到 %s", server)),
                huh.NewInput().
                    Key("token").
                    Title("Token").
                    EchoMode(huh.EchoModePassword).
                    Validate(func(s string) error {
                        if s == "" {
                            return errors.New("token 不能为空")
                        }
                        return nil
                    }),
            ),
        ).WithTheme(huh.ThemeCharm()),
    }
}

// ServiceSelector - 本地服务选择器
type ServiceSelector struct {
    list list.Model
}

type ServiceItem struct {
    Port     int
    Protocol string
    Info     string // 检测到的服务类型
}

func (i ServiceItem) FilterValue() string { 
    return fmt.Sprintf("%d %s", i.Port, i.Info) 
}
func (i ServiceItem) Title() string       { 
    return fmt.Sprintf("%d", i.Port) 
}
func (i ServiceItem) Description() string { 
    return i.Info 
}

// ServerSelector - Server 选择器
type ServerSelector struct {
    list list.Model
}

// ConnectingSpinner - 连接状态
type ConnectingSpinner struct {
    spinner spinner.Model
    steps   []string
    current int
}

// StatusTable - 状态展示
type StatusTable struct {
    table table.Model
}

// MainModel - 主 TUI 模型
type MainModel struct {
    state      AppState
    tunnel     *tunnel.Client
    
    // 子组件
    authForm   *AuthForm
    serviceSel *ServiceSelector
    serverSel  *ServerSelector
    spinner    *ConnectingSpinner
    statusTable *StatusTable
    
    // 数据
    tunnels    []*tunnel.Tunnel
    err        error
}

type AppState int

const (
    StateInit AppState = iota
    StateSelectingServer
    StateAuth
    StateSelectingService
    StateConnecting
    StateRunning
    StateError
)
```

### 5.2 交互流程

```
用户输入: gnar

┌─────────────────────────────────────────────────────────────┐
│  StateInit                                                   │
│  1. 检查是否有默认 server                                    │
│     - 有 -> 检查是否已认证 -> 是 -> StateSelectingService   │
│     - 无 -> StateSelectingServer                            │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│  StateSelectingServer                                        │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ 🖥️  选择 Server                                        │  │
│  │                                                        │  │
│  │ > gnar.example.com (默认)                             │  │
│  │   dev.gnar.com                                        │  │
│  │   [添加新 Server...]                                  │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│  StateAuth                                                   │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ 🔐 认证到 gnar.example.com                             │  │
│  │                                                        │  │
│  │ Token: [***************************]                  │  │
│  │                                                        │  │
│  │   [  验证  ]      [  取消  ]                          │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│  StateSelectingService                                       │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ 🖥️  选择要暴露的本地服务 (共 3 个)                      │  │
│  │                                                        │  │
│  │ > 3000  Next.js dev server (http)                     │  │
│  │   8080  Spring Boot (http)                            │  │
│  │   5432  PostgreSQL (tcp)                              │  │
│  │   ─────────────────────                               │  │
│  │   自定义端口...                                       │  │
│  │                                                        │  │
│  │ 按 / 搜索, Enter 选择, q 退出                         │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│  StateConnecting                                             │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ ⠋ 正在连接 gnar.example.com...                         │  │
│  │                                                        │  │
│  │  ⠙ 认证检查...                                        │  │
│  │  ⠹ 连接 server...                                     │  │
│  │  ⠸ 协商端口...                                        │  │
│  │  ⠼ 注册域名...                                        │  │
│  │  ⠴ 建立隧道...                                        │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│  StateRunning                                                │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ ✅ 服务已暴露                                          │  │
│  │                                                        │  │
│  │ ┌──────────┬──────────────────────────────────────┐   │  │
│  │ │ 本地     │ localhost:3000                       │   │  │
│  │ ├──────────┼──────────────────────────────────────┤   │  │
│  │ │ 公网     │ https://myapp.abc123.gnar.example.com│   │  │
│  │ ├──────────┼──────────────────────────────────────┤   │  │
│  │ │ Server   │ gnar.example.com                     │   │  │
│  │ ├──────────┼──────────────────────────────────────┤   │  │
│  │ │ 状态     │ ● 活跃                               │   │  │
│  │ ├──────────┼──────────────────────────────────────┤   │  │
│  │ │ 流量     │ ↑ 1.2 MB  ↓ 5.6 MB                   │   │  │
│  │ └──────────┴──────────────────────────────────────┘   │  │
│  │                                                        │  │
│  │  按 Ctrl+C 停止   │   按 c 复制 URL   │   按 q 退出  │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

## 6. 存储设计

### 6.1 本地凭证存储

```go
// macOS: Keychain
// Linux: Secret Service API / pass
// Windows: Windows Credential Manager

type KeyringStore struct{}

func (s *KeyringStore) Save(cred *Credential) error {
    // service: "gnar"
    // account: cred.Server
    // secret:  cred.RefreshToken (加密后)
}

// 配置缓存 (~/.gnar/config.db)
// SQLite 存储 server 列表、默认 server 等元数据
```

### 6.2 Server 端存储

```go
// 单机内存存储 + 可选持久化
// map[string]*ServerTunnel
// map[string]*DomainInfo
// map[string]*TokenInfo
```

## 7. 项目结构

```
gnar/
├── go.mod
├── Makefile
├── README.md
├── DESIGN.md                          # 本文件
│
├── pkg/tunnel/                        # 🎯 独立库
│   ├── tunnel.go                      # 接口定义
│   ├── client.go                      # 客户端实现
│   ├── server.go                      # 服务端实现
│   ├── auth.go                        # 认证管理
│   ├── config.go                      # 配置结构
│   ├── errors.go                      # 错误定义
│   ├── events.go                      # 事件系统
│   │
│   ├── protocol/
│   │   ├── packet.go                  # 协议消息
│   │   └── quic.go                    # QUIC 传输
│   │
│   ├── internal/
│   │   ├── auth/
│   │   │   ├── manager.go
│   │   │   └── store.go
│   │   ├── port/
│   │   │   └── manager.go
│   │   └── domain/
│   │       └── manager.go
│   │
│   └── types/
│       └── types.go                   # 公共类型
│
├── internal/                          # 内部实现
│   ├── cli/                           # 命令行
│   │   ├── root.go
│   │   ├── auth.go
│   │   └── expose.go
│   │
│   ├── tui/                           # TUI 层
│   │   ├── app.go                     # 主程序
│   │   ├── styles.go                  # 样式
│   │   ├── components/
│   │   │   ├── auth_form.go
│   │   │   ├── service_selector.go
│   │   │   ├── server_selector.go
│   │   │   ├── connecting_spinner.go
│   │   │   └── status_table.go
│   │   └── views/
│   │       ├── auth_view.go
│   │       ├── select_view.go
│   │       └── status_view.go
│   │
│   ├── discover/                      # 服务发现
│   │   └── scanner.go
│   │
│   └── store/                         # 本地存储
│       └── sqlite.go
│
├── cmd/
│   ├── gnar/                          # 客户端 CLI
│   │   └── main.go
│   │
│   └── gnar-server/                   # 服务端
│       └── main.go
│
└── configs/                           # 配置文件示例
    ├── client.yaml
    └── server.yaml
```

## 8. 实现阶段

### Phase 1: 基础架构 (Week 1)
- [ ] 创建新分支 `v2-rewrite`
- [ ] 更新 go.mod，引入依赖
- [ ] 定义 pkg/tunnel 接口
- [ ] 实现 errors、events、types 基础

### Phase 2: pkg/tunnel 库 (Week 2-3)
- [ ] protocol/packet 协议定义
- [ ] QUIC 传输层实现
- [ ] Client 实现（连接、认证、Expose）
- [ ] Server 实现（监听、认证处理）
- [ ] Auth 系统（OAuth flow、自动刷新）
- [ ] Port/Domain Manager

### Phase 3: TUI 层 (Week 3-4)
- [ ] 基础组件（styles、布局）
- [ ] AuthForm 组件
- [ ] ServiceSelector 组件
- [ ] ConnectingSpinner 组件
- [ ] StatusTable 组件
- [ ] MainModel 状态机
- [ ] 集成 tunnel 库

### Phase 4: Server (Week 4)
- [ ] HTTPS Router (autocert)
- [ ] Auth API
- [ ] Tunnel Manager
- [ ] 流量统计

### Phase 5: 集成与清理 (Week 5)
- [ ] CLI 命令实现
- [ ] 端到端测试
- [ ] 删除旧代码
- [ ] 文档更新

## 9. 依赖列表

```go
// go.mod

require (
    // TUI
    github.com/charmbracelet/bubbletea v1.1.0
    github.com/charmbracelet/bubbles v0.20.0
    github.com/charmbracelet/lipgloss v0.13.0
    github.com/charmbracelet/huh v0.6.0
    
    // CLI
    github.com/spf13/cobra v1.8.0
    
    // Transport
    github.com/quic-go/quic-go v0.45.0
    
    // HTTPS
    golang.org/x/crypto v0.25.0
    golang.org/x/net v0.27.0
    
    // Storage
    github.com/zalando/go-keyring v0.2.4
    github.com/mattn/go-sqlite3 v1.14.22
    
    // Utils
    github.com/google/uuid v1.6.0
    github.com/pkg/errors v0.9.1
    
    // Testing
    github.com/stretchr/testify v1.9.0
)
```

## 10. 关键决策记录

### ADR 1: 为什么用 QUIC 而非 TCP+TLS?
- QUIC 内建 TLS 1.3，无需额外处理加密
- 0-RTT 连接恢复，重连更快
- 多路复用 Streams，替代 yamux
- 更好的拥塞控制

### ADR 2: 为什么分离 pkg/tunnel 和 internal/tui?
- 库可独立使用，方便二次开发
- TUI 只是其中一种交互方式
- 清晰的依赖边界

### ADR 3: 为什么用 SQLite + Keyring 而非纯文件?
- Keyring 更安全（系统级加密）
- SQLite 方便查询和扩展
- 跨平台兼容性好

### ADR 4: 为什么 OAuth2 风格而非简单 Token?
- Access Token 短期有效，更安全
- Refresh Token 长期存储，用户体验好
- 自动刷新无感知
- 符合安全最佳实践

## 11. 测试策略

```go
// 单元测试
- pkg/tunnel/*_test.go
- internal/tui/*_test.go

// 集成测试
- tests/integration/client_server_test.go
- tests/integration/auth_test.go

// E2E 测试
- tests/e2e/expose_test.go
```

## 12. 文档清单

- [ ] README.md (快速开始)
- [ ] docs/ARCHITECTURE.md (架构详解)
- [ ] docs/API.md (库 API 文档)
- [ ] docs/PROTOCOL.md (协议规范)
- [ ] docs/DEPLOYMENT.md (Server 部署指南)
- [ ] CHANGELOG.md

---

**设计完成日期**: 2026-01-30
**版本**: v2.0.0-draft
**状态**: 等待评审
