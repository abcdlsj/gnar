use std::io::{self, IsTerminal, Write};

use serde::Serialize;

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
    TunnelReady {
        public_url: &'a str,
        target: &'a str,
        account: Option<&'a str>,
        reserved: bool,
    },
    EdgeReconnecting,
    EdgeRestored,
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
