use std::net::SocketAddr;
use std::path::PathBuf;
use std::time::Duration;

use clap::{Args, Parser, Subcommand};

use crate::protocol::ForwardSettings;

#[derive(Debug, Parser)]
#[command(
    name = "gnar",
    version,
    about = "Publish and inspect a local HTTP service"
)]
pub struct Cli {
    #[command(subcommand)]
    pub command: Option<Command>,

    #[arg(value_name = "TARGET")]
    pub target: Option<String>,

    #[arg(long, global = true, help = "Emit newline-delimited JSON events")]
    pub json: bool,

    #[arg(
        long,
        global = true,
        help = "Use streaming plain output instead of the terminal UI"
    )]
    pub no_tui: bool,

    #[arg(
        long,
        global = true,
        env = "GNAR_EDGE",
        value_parser = parse_edge,
        help = "Edge to use; a bare host defaults to http://"
    )]
    pub edge: Option<String>,

    #[arg(long, global = true)]
    pub name: Option<String>,

    #[arg(
        long,
        global = true,
        num_args = 1,
        value_parser = clap::builder::BoolishValueParser::new(),
        default_value_t = true,
        help = "Preserve the original Host header when forwarding"
    )]
    pub preserve_host: bool,

    #[arg(
        long,
        global = true,
        num_args = 1,
        value_parser = clap::builder::BoolishValueParser::new(),
        default_value_t = true,
        help = "Forward WebSocket connections"
    )]
    pub websocket: bool,

    #[arg(
        long,
        global = true,
        default_value_t = 16,
        help = "Maximum request body in MiB"
    )]
    pub max_request_mib: u64,

    #[arg(
        long,
        global = true,
        default_value_t = 30,
        help = "Response head timeout in seconds"
    )]
    pub response_timeout_secs: u64,

    #[arg(
        long,
        global = true,
        default_value_t = 64,
        help = "Maximum concurrent exchanges"
    )]
    pub max_concurrent: usize,

    #[arg(
        long,
        global = true,
        default_value_t = 600,
        help = "Requests per minute for this tunnel"
    )]
    pub requests_per_minute: u32,
}

impl Cli {
    pub fn settings(&self) -> ForwardSettings {
        ForwardSettings {
            preserve_host: self.preserve_host,
            websocket: self.websocket,
            max_request_bytes: self.max_request_mib.saturating_mul(1024 * 1024),
            response_head_timeout_ms: self.response_timeout_secs.saturating_mul(1000),
            max_concurrent_exchanges: self.max_concurrent,
            requests_per_minute: self.requests_per_minute,
        }
    }
}

fn parse_edge(value: &str) -> Result<String, String> {
    let value = value.trim().trim_end_matches('/');
    if value.is_empty() {
        return Err("an edge URL is required".into());
    }
    let candidate = if value.contains("://") {
        value.to_string()
    } else {
        format!("http://{value}")
    };

    let url =
        url::Url::parse(&candidate).map_err(|error| format!("{value} is not a URL: {error}"))?;
    if !matches!(url.scheme(), "http" | "https") {
        return Err(format!(
            "{value} must use http:// or https://, not {}://",
            url.scheme()
        ));
    }
    if url.host_str().is_none_or(str::is_empty) {
        return Err(format!("{value} is missing a host"));
    }
    if !url.username().is_empty() || url.password().is_some() {
        return Err(
            "edge URLs must not contain credentials; pass enrollment keys via stdin".into(),
        );
    }
    if url.query().is_some() || url.fragment().is_some() {
        return Err("edge URLs must not contain a query or fragment".into());
    }
    Ok(candidate)
}

#[cfg(test)]
mod tests {
    use clap::Parser;

    use super::{Cli, Command, parse_edge};

    #[test]
    fn a_bare_host_and_port_becomes_an_http_url() {
        assert_eq!(
            parse_edge("127.0.0.1:8910").unwrap(),
            "http://127.0.0.1:8910"
        );
        assert_eq!(
            parse_edge("localhost:8910").unwrap(),
            "http://localhost:8910"
        );
    }

    #[test]
    fn an_explicit_scheme_and_trailing_slash_are_preserved_or_trimmed() {
        assert_eq!(
            parse_edge("https://gnar.example.com/").unwrap(),
            "https://gnar.example.com"
        );
        assert_eq!(
            parse_edge(" http://127.0.0.1:8910 ").unwrap(),
            "http://127.0.0.1:8910"
        );
    }

    #[test]
    fn a_non_http_scheme_is_rejected_before_any_request() {
        assert!(
            parse_edge("ws://127.0.0.1:8910")
                .unwrap_err()
                .contains("http")
        );
        assert!(parse_edge("").is_err());
    }

    #[test]
    fn an_edge_url_cannot_carry_credentials() {
        assert!(parse_edge("https://user:password@gnar.example.com").is_err());
    }

    #[test]
    fn enrollment_login_requires_an_account_and_stdin_flag() {
        let cli = Cli::try_parse_from([
            "gnar",
            "login",
            "--edge",
            "https://gnar.example.com/base/",
            "--account",
            "alice",
            "--enrollment-key-stdin",
            "--json",
        ])
        .unwrap();
        match cli.command {
            Some(Command::Login(args)) => {
                assert_eq!(args.account.as_deref(), Some("alice"));
                assert!(args.enrollment_key_stdin);
                assert!(cli.json);
                assert_eq!(cli.edge.as_deref(), Some("https://gnar.example.com/base"));
            }
            _ => panic!("expected login command"),
        }
    }

    #[test]
    fn interactive_login_stays_a_plain_login_without_enrollment_flags() {
        let cli = Cli::try_parse_from(["gnar", "login"]).unwrap();
        assert!(
            matches!(cli.command, Some(Command::Login(args)) if args.account.is_none() && !args.enrollment_key_stdin)
        );
    }
}

#[derive(Debug, Subcommand)]
pub enum Command {
    /// Sign in to an edge and store its token
    Login(LoginArgs),
    /// Forget the stored token for an edge
    Logout,
    /// Show the signed-in account for an edge
    Whoami,
    /// Give up a reserved name so another account can take it
    Release {
        #[arg(value_name = "NAME")]
        name: String,
    },
    /// Run a self-hosted edge
    Serve(ServeArgs),
    /// Print version information
    Version,
}

#[derive(Clone, Debug, Args)]
pub struct LoginArgs {
    #[arg(
        long,
        value_name = "ACCOUNT",
        requires = "enrollment_key_stdin",
        help = "Account name to enroll when reading the enrollment key from stdin"
    )]
    pub account: Option<String>,

    #[arg(
        long,
        requires = "account",
        help = "Read one enrollment key from stdin instead of opening the device flow"
    )]
    pub enrollment_key_stdin: bool,
}

#[derive(Clone, Debug, Args)]
pub struct ServeArgs {
    #[arg(long, default_value = "127.0.0.1:8910")]
    pub listen: SocketAddr,

    #[arg(long, default_value = "http://127.0.0.1:8910")]
    pub public_url: String,

    #[arg(long)]
    pub base_domain: Option<String>,

    #[arg(long, default_value = "gnar.db")]
    pub database: PathBuf,

    #[arg(
        long,
        help = "Allow binding a non-loopback address, exposing this edge to the network"
    )]
    pub allow_public_bind: bool,

    #[arg(
        long,
        env = "GNAR_APPROVAL_SECRET",
        help = "Secret required to approve a device code and create an account"
    )]
    pub approval_secret: Option<String>,

    #[arg(
        long,
        conflicts_with = "approval_secret",
        help = "Serve anonymous tunnels only, without asking; no accounts can be created"
    )]
    pub anonymous_only: bool,

    #[arg(long, default_value_t = 3, help = "Concurrent tunnels per account")]
    pub account_tunnels: usize,

    #[arg(long, default_value_t = 600, help = "Requests per minute per tunnel")]
    pub account_requests: u32,

    #[arg(long, default_value_t = 1, help = "Concurrent anonymous tunnels")]
    pub anonymous_tunnels: usize,

    #[arg(
        long,
        default_value_t = 120,
        help = "Requests per minute for an anonymous tunnel"
    )]
    pub anonymous_requests: u32,

    #[arg(
        long,
        default_value_t = crate::protocol::WS_CONCURRENT,
        help = "Maximum concurrent WebSocket exchanges per tunnel"
    )]
    pub websocket_concurrent: usize,

    #[arg(
        long,
        default_value_t = crate::protocol::WS_IDLE_TIMEOUT_SECS,
        help = "Close WebSocket connections that do not answer heartbeats"
    )]
    pub websocket_idle_timeout_secs: u64,

    #[arg(
        long,
        default_value_t = crate::protocol::WS_BYTES_PER_MINUTE_MIB,
        help = "Maximum WebSocket payload per connection per minute in MiB"
    )]
    pub websocket_bytes_per_minute_mib: u64,

    #[arg(
        long,
        default_value_t = crate::protocol::WS_FRAMES_PER_MINUTE,
        help = "Maximum WebSocket frames per connection per minute"
    )]
    pub websocket_frames_per_minute: u64,
}

impl ServeArgs {
    pub fn quota(&self, authenticated: bool) -> Quota {
        if authenticated {
            Quota {
                tunnels: self.account_tunnels,
                requests_per_minute: self.account_requests,
            }
        } else {
            Quota {
                tunnels: self.anonymous_tunnels,
                requests_per_minute: self.anonymous_requests,
            }
        }
    }

    pub fn websocket_concurrent(&self) -> usize {
        self.websocket_concurrent.clamp(1, 512)
    }

    pub fn websocket_idle_timeout(&self) -> Duration {
        Duration::from_secs(self.websocket_idle_timeout_secs.clamp(1, 24 * 60 * 60))
    }

    pub fn websocket_bytes_per_minute(&self) -> u64 {
        self.websocket_bytes_per_minute_mib
            .clamp(1, 1024 * 1024)
            .saturating_mul(1024 * 1024)
    }

    pub fn websocket_frames_per_minute(&self) -> u64 {
        self.websocket_frames_per_minute.clamp(1, 10_000_000)
    }
}

#[derive(Clone, Copy, Debug)]
pub struct Quota {
    pub tunnels: usize,
    pub requests_per_minute: u32,
}
