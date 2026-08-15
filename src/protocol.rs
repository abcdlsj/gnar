use serde::{Deserialize, Serialize};

pub const VERSION: u16 = 2;
pub const MAX_NAME_LENGTH: usize = 48;

pub const MIN_MAX_REQUEST_BYTES: u64 = 1024 * 1024;
pub const MAX_MAX_REQUEST_BYTES: u64 = 256 * 1024 * 1024;
pub const MIN_RESPONSE_TIMEOUT_MS: u64 = 1_000;
pub const MAX_RESPONSE_TIMEOUT_MS: u64 = 300_000;
pub const MIN_MAX_CONCURRENT_EXCHANGES: usize = 1;
pub const MAX_MAX_CONCURRENT_EXCHANGES: usize = 512;
pub const MIN_REQUESTS_PER_MINUTE: u32 = 1;
pub const MAX_REQUESTS_PER_MINUTE: u32 = 100_000;

pub type Header = (String, Vec<u8>);

/// Per-tunnel forwarding limits chosen by the client. The edge clamps these to
/// its own safety bounds and reports the effective values back to the client.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ForwardSettings {
    pub preserve_host: bool,
    pub websocket: bool,
    pub max_request_bytes: u64,
    pub response_head_timeout_ms: u64,
    pub max_concurrent_exchanges: usize,
    pub requests_per_minute: u32,
}

impl Default for ForwardSettings {
    fn default() -> Self {
        Self {
            preserve_host: true,
            websocket: true,
            max_request_bytes: 16 * 1024 * 1024,
            response_head_timeout_ms: 30_000,
            max_concurrent_exchanges: 64,
            requests_per_minute: 600,
        }
    }
}

impl ForwardSettings {
    pub fn clamped(mut self) -> Self {
        self.max_request_bytes = self
            .max_request_bytes
            .clamp(MIN_MAX_REQUEST_BYTES, MAX_MAX_REQUEST_BYTES);
        self.response_head_timeout_ms = self
            .response_head_timeout_ms
            .clamp(MIN_RESPONSE_TIMEOUT_MS, MAX_RESPONSE_TIMEOUT_MS);
        self.max_concurrent_exchanges = self
            .max_concurrent_exchanges
            .clamp(MIN_MAX_CONCURRENT_EXCHANGES, MAX_MAX_CONCURRENT_EXCHANGES);
        self.requests_per_minute = self
            .requests_per_minute
            .clamp(MIN_REQUESTS_PER_MINUTE, MAX_REQUESTS_PER_MINUTE);
        self
    }
}

/// A single WebSocket message relayed between a public client and the local
/// service. Text and binary payloads are passed through unchanged.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum WsMessage {
    Text(String),
    Binary(Vec<u8>),
    Ping(Vec<u8>),
    Pong(Vec<u8>),
    /// `code: None` mirrors a WebSocket close frame without a status code.
    Close {
        code: Option<u16>,
        reason: String,
    },
}

impl WsMessage {
    pub fn len(&self) -> usize {
        match self {
            Self::Text(text) => text.len(),
            Self::Binary(bytes) | Self::Ping(bytes) | Self::Pong(bytes) => bytes.len(),
            Self::Close { reason, .. } => reason.len(),
        }
    }

    pub fn is_close(&self) -> bool {
        matches!(self, Self::Close { .. })
    }
}

#[derive(Debug, Serialize, Deserialize)]
pub struct OpenTunnel {
    pub version: u16,
    pub name: Option<String>,
    pub token: Option<String>,
    pub settings: ForwardSettings,
}

#[derive(Debug, Serialize, Deserialize)]
pub enum OpenResult {
    Opened(TunnelOpened),
    Rejected { reason: String },
}

#[derive(Debug, Serialize, Deserialize)]
pub struct TunnelOpened {
    pub version: u16,
    pub name: String,
    pub public_url: String,
    pub account: Option<String>,
    pub reserved: bool,
    pub settings: ForwardSettings,
}

#[derive(Debug, Serialize, Deserialize)]
pub enum EdgeFrame {
    RequestStart {
        id: u64,
        method: String,
        path: String,
        headers: Vec<Header>,
    },
    RequestChunk {
        id: u64,
        body: Vec<u8>,
    },
    RequestEnd {
        id: u64,
    },
    /// Ask the client to open a WebSocket to the local service.
    WsStart {
        id: u64,
        path: String,
        headers: Vec<Header>,
        protocol: Option<String>,
    },
    /// A frame from the public WebSocket client headed to the local service.
    WsFrame {
        id: u64,
        message: WsMessage,
    },
    Cancel {
        id: u64,
    },
}

#[derive(Debug, Serialize, Deserialize)]
pub enum ClientFrame {
    Start {
        id: u64,
        status: u16,
        headers: Vec<Header>,
    },
    Chunk {
        id: u64,
        body: Vec<u8>,
    },
    End {
        id: u64,
    },
    Error {
        id: u64,
        reason: String,
    },
    /// A frame from the local WebSocket service headed to the public client.
    WsFrame {
        id: u64,
        message: WsMessage,
    },
}

pub fn encode<T: Serialize>(value: &T) -> Result<Vec<u8>, postcard::Error> {
    postcard::to_allocvec(value)
}

pub fn decode<'a, T: Deserialize<'a>>(bytes: &'a [u8]) -> Result<T, postcard::Error> {
    postcard::from_bytes(bytes)
}

pub fn hop_by_hop_header(name: &str) -> bool {
    matches!(
        name.to_ascii_lowercase().as_str(),
        "connection"
            | "keep-alive"
            | "proxy-authenticate"
            | "proxy-authorization"
            | "te"
            | "trailer"
            | "transfer-encoding"
            | "upgrade"
    )
}

#[cfg(test)]
mod tests {
    use super::{
        ClientFrame, EdgeFrame, ForwardSettings, OpenResult, OpenTunnel, TunnelOpened, WsMessage,
        decode, encode,
    };

    fn settings() -> ForwardSettings {
        ForwardSettings {
            preserve_host: false,
            websocket: true,
            max_request_bytes: 8 * 1024 * 1024,
            response_head_timeout_ms: 12_000,
            max_concurrent_exchanges: 32,
            requests_per_minute: 300,
        }
    }

    #[test]
    fn settings_clamp_to_configured_bounds() {
        let clamped = ForwardSettings {
            preserve_host: true,
            websocket: true,
            max_request_bytes: 0,
            response_head_timeout_ms: 0,
            max_concurrent_exchanges: 0,
            requests_per_minute: 0,
        }
        .clamped();

        assert_eq!(clamped.max_request_bytes, super::MIN_MAX_REQUEST_BYTES);
        assert_eq!(
            clamped.response_head_timeout_ms,
            super::MIN_RESPONSE_TIMEOUT_MS
        );
        assert_eq!(
            clamped.max_concurrent_exchanges,
            super::MIN_MAX_CONCURRENT_EXCHANGES
        );
        assert_eq!(clamped.requests_per_minute, super::MIN_REQUESTS_PER_MINUTE);
    }

    #[test]
    fn open_tunnel_round_trips_settings() {
        let open = OpenTunnel {
            version: super::VERSION,
            name: Some("demo".into()),
            token: None,
            settings: settings(),
        };
        let decoded = decode::<OpenTunnel>(&encode(&open).unwrap()).unwrap();

        assert_eq!(decoded.version, super::VERSION);
        assert_eq!(decoded.settings, settings());
    }

    #[test]
    fn opened_tunnel_reports_effective_settings() {
        let opened = OpenResult::Opened(TunnelOpened {
            version: super::VERSION,
            name: "demo".into(),
            public_url: "https://demo.example.com".into(),
            account: Some("alice".into()),
            reserved: true,
            settings: settings(),
        });
        let decoded = decode::<OpenResult>(&encode(&opened).unwrap()).unwrap();

        match decoded {
            OpenResult::Opened(opened) => assert_eq!(opened.settings, settings()),
            OpenResult::Rejected { .. } => panic!("expected an opened tunnel"),
        }
    }

    #[test]
    fn websocket_frames_round_trip() {
        let edge = EdgeFrame::WsFrame {
            id: 7,
            message: WsMessage::Binary(vec![1, 2, 3]),
        };
        let decoded = decode::<EdgeFrame>(&encode(&edge).unwrap()).unwrap();
        assert_eq!(decoded.message_len(), 3);

        let client = ClientFrame::WsFrame {
            id: 8,
            message: WsMessage::Close {
                code: Some(1000),
                reason: "done".into(),
            },
        };
        let decoded = decode::<ClientFrame>(&encode(&client).unwrap()).unwrap();
        assert!(decoded.message_is_close());

        let bare = ClientFrame::WsFrame {
            id: 9,
            message: WsMessage::Close {
                code: None,
                reason: String::new(),
            },
        };
        let decoded = decode::<ClientFrame>(&encode(&bare).unwrap()).unwrap();
        assert!(decoded.message_is_close());
    }

    #[test]
    fn ws_start_carries_selected_protocol() {
        let frame = EdgeFrame::WsStart {
            id: 1,
            path: "/v1/ws".into(),
            headers: vec![("origin".into(), b"https://demo.example.com".to_vec())],
            protocol: Some("chat".into()),
        };
        let decoded = decode::<EdgeFrame>(&encode(&frame).unwrap()).unwrap();

        match decoded {
            EdgeFrame::WsStart { protocol, .. } => assert_eq!(protocol.as_deref(), Some("chat")),
            _ => panic!("expected WsStart"),
        }
    }

    impl EdgeFrame {
        fn message_len(&self) -> usize {
            match self {
                Self::WsFrame { message, .. } => message.len(),
                _ => 0,
            }
        }
    }

    impl ClientFrame {
        fn message_is_close(&self) -> bool {
            matches!(self, Self::WsFrame { message, .. } if message.is_close())
        }
    }
}
