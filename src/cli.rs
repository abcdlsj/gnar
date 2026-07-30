use std::net::SocketAddr;
use std::path::PathBuf;

use clap::{Args, Parser, Subcommand};

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
    Ok(candidate)
}

#[cfg(test)]
mod tests {
    use super::parse_edge;

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
}

#[derive(Debug, Subcommand)]
pub enum Command {
    /// Sign in to an edge and store its token
    Login,
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
}

#[derive(Clone, Copy, Debug)]
pub struct Quota {
    pub tunnels: usize,
    pub requests_per_minute: u32,
}
