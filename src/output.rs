use std::io::{self, IsTerminal, Write};

use serde::Serialize;

#[derive(Debug, Serialize)]
pub struct KeySummary {
    pub name: String,
    pub account: String,
    pub max_uses: u32,
    pub expires_at: Option<i64>,
}

#[derive(Debug, Serialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum Event<'a> {
    Discovering,
    LocalServiceFound {
        target: &'a str,
        kind: &'a str,
        detail: Option<&'a str>,
    },
    TargetSelected {
        target: &'a str,
    },
    LocalReady {
        target: &'a str,
        status: u16,
        latency_ms: u128,
    },
    LocalUnavailable {
        target: &'a str,
        reason: &'a str,
    },
    Connecting {
        edge: &'a str,
    },
    TunnelReady {
        public_url: &'a str,
        target: &'a str,
        account: Option<&'a str>,
        reserved: bool,
    },
    EdgeReconnecting,
    EdgeRestored,
    EnrollmentStarted {
        account: &'a str,
    },
    InviteEnrollmentStarted,
    EnrollmentSucceeded {
        account: &'a str,
    },
    KeyAdded {
        name: &'a str,
        account: &'a str,
        max_uses: u32,
        expires_at: Option<i64>,
        secret: Option<&'a str>,
    },
    KeyList {
        keys: Vec<KeySummary>,
    },
    KeyRevoked {
        name: &'a str,
    },
    KeyShown {
        name: &'a str,
        secret: &'a str,
    },
}

#[derive(Clone, Copy)]
pub struct Output {
    json: bool,
    interactive: bool,
}

impl Output {
    pub fn new(json: bool, no_tui: bool) -> Self {
        let interactive =
            !json && !no_tui && io::stdin().is_terminal() && io::stdout().is_terminal();
        Self { json, interactive }
    }

    pub fn interactive(&self) -> bool {
        self.interactive
    }

    pub fn event(&self, event: Event<'_>) -> io::Result<()> {
        let stdout = io::stdout();
        let mut writer = stdout.lock();

        if self.json {
            serde_json::to_writer(&mut writer, &event)?;
            writeln!(writer)?;
            return Ok(());
        }

        match event {
            Event::Discovering => writeln!(writer, "Searching for a local HTTP service"),
            Event::LocalServiceFound {
                target,
                kind,
                detail,
            } => match detail {
                Some(detail) => writeln!(writer, "Found   {kind} at {target} ({detail})"),
                None => writeln!(writer, "Found   {kind} at {target}"),
            },
            Event::TargetSelected { target } => writeln!(writer, "Target  {target}"),
            Event::LocalReady {
                target,
                status,
                latency_ms,
            } => writeln!(
                writer,
                "Ready   {target} returned HTTP {status} in {latency_ms}ms"
            ),
            Event::LocalUnavailable { target, reason } => {
                writeln!(writer, "Failed  {target} is unavailable: {reason}")
            }
            Event::Connecting { edge } => {
                writeln!(writer, "Connecting to {edge}")
            }
            Event::TunnelReady {
                public_url,
                target,
                account,
                reserved,
            } => {
                if self.interactive {
                    return Ok(());
                }
                let ownership = match (account, reserved) {
                    (Some(account), true) => format!(" (reserved by {account})"),
                    (Some(account), false) => format!(" (signed in as {account})"),
                    (None, _) => String::new(),
                };
                writeln!(
                    writer,
                    "Public  {public_url}{ownership}\nForward {target}\nPress Ctrl+C to stop"
                )
            }
            Event::EdgeReconnecting => {
                if self.interactive {
                    return Ok(());
                }
                writeln!(writer, "Edge    disconnected, reconnecting")
            }
            Event::EdgeRestored => {
                if self.interactive {
                    return Ok(());
                }
                writeln!(writer, "Edge    connection restored")
            }
            Event::EnrollmentStarted { account } => {
                writeln!(writer, "Enroll  account {account}")
            }
            Event::InviteEnrollmentStarted => {
                writeln!(writer, "Enroll  invite key")
            }
            Event::EnrollmentSucceeded { account } => {
                writeln!(writer, "Signed  in as {account}")
            }
            Event::KeyAdded {
                name,
                account,
                max_uses,
                expires_at,
                secret,
            } => {
                let expires = expires_at
                    .map(|epoch| epoch.to_string())
                    .unwrap_or_else(|| "never".into());
                writeln!(
                    writer,
                    "Key {name} -> account {account}, max {max_uses} uses, expires {expires}"
                )?;
                match secret {
                    Some(secret) => {
                        writeln!(writer, "Secret: {secret}")?;
                        writeln!(
                            writer,
                            "Share securely: gnar login --edge <EDGE_URL> --key-stdin < secret.txt"
                        )
                    }
                    None => writeln!(
                        writer,
                        "Secret stored in the keys file; run `gnar key show {name}` to display it"
                    ),
                }
            }
            Event::KeyList { keys } => {
                if keys.is_empty() {
                    writeln!(writer, "No invite keys configured")?;
                } else {
                    for key in keys {
                        let expires = key
                            .expires_at
                            .map(|epoch| epoch.to_string())
                            .unwrap_or_else(|| "never".into());
                        writeln!(
                            writer,
                            "{}\taccount {}\tmax {} uses\texpires {expires}",
                            key.name, key.account, key.max_uses
                        )?;
                    }
                }
                Ok(())
            }
            Event::KeyRevoked { name } => {
                writeln!(writer, "Removed invite key {name}")
            }
            Event::KeyShown { name, secret } => {
                writeln!(writer, "Secret for {name}: {secret}")
            }
        }
    }

    pub fn error(&self, error: &dyn std::fmt::Display) {
        if !self.json {
            eprintln!("error: {error}");
            return;
        }
        let stdout = io::stdout();
        let mut writer = stdout.lock();
        let value = serde_json::json!({ "type": "error", "message": error.to_string() });
        let _ = writeln!(writer, "{value}");
    }
}
