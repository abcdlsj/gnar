use serde::{Deserialize, Serialize};

pub const VERSION: u16 = 1;
pub const MAX_NAME_LENGTH: usize = 48;

pub type Header = (String, Vec<u8>);

#[derive(Debug, Serialize, Deserialize)]
pub struct OpenTunnel {
    pub version: u16,
    pub name: Option<String>,
    pub token: Option<String>,
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
