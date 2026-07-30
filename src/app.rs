use std::fmt;
use std::time::{Duration, Instant};

use reqwest::Client;
use url::Url;

use crate::discover;
use crate::output::{Event, Output};

const PROBE_TIMEOUT: Duration = Duration::from_secs(3);

pub struct App {
    output: Output,
    client: Client,
}

impl App {
    pub fn new(output: Output) -> Self {
        let client = Client::builder()
            .timeout(PROBE_TIMEOUT)
            .redirect(reqwest::redirect::Policy::limited(5))
            .build()
            .expect("valid HTTP client configuration");

        Self { output, client }
    }

    pub async fn run(
        &self,
        input: Option<String>,
        edge: String,
        name: Option<String>,
    ) -> Result<(), AppError> {
        let target = match input {
            Some(input) => Target::parse(&input)?,
            None => {
                self.output.event(Event::Discovering)?;
                let mut services = discover::local_services(&self.client).await?;
                let ambiguous = services.len() > 1;
                let selected = if self.output.interactive() && ambiguous {
                    match crate::ui::choose_service(&services).map_err(AppError::Output)? {
                        Some(selected) => selected,
                        None => return Ok(()),
                    }
                } else {
                    0
                };
                let service = services.remove(selected);
                if !(self.output.interactive() && ambiguous) {
                    self.output.event(Event::LocalServiceFound {
                        target: service.url.as_str(),
                        kind: &service.kind,
                        detail: service.detail.as_deref(),
                    })?;
                }
                Target(service.url)
            }
        };
        let target_url = target.as_str();

        self.output
            .event(Event::TargetSelected { target: target_url })?;

        let started = Instant::now();
        match self.client.get(target_url).send().await {
            Ok(response) => {
                self.output.event(Event::LocalReady {
                    target: target_url,
                    status: response.status().as_u16(),
                    latency_ms: started.elapsed().as_millis(),
                })?;
                crate::tunnel::run(target.0, edge, name, &self.output).await
            }
            Err(error) => {
                let reason = probe_reason(&error);
                self.output.event(Event::LocalUnavailable {
                    target: target_url,
                    reason: &reason,
                })?;
                Err(AppError::Unavailable)
            }
        }
    }
}

#[derive(Debug, PartialEq)]
struct Target(Url);

impl Target {
    fn parse(input: &str) -> Result<Self, AppError> {
        let input = input.trim();
        if input.is_empty() {
            return Err(AppError::InvalidTarget("target cannot be empty".into()));
        }

        if let Ok(port) = input.parse::<u16>() {
            if port == 0 {
                return Err(AppError::InvalidTarget(
                    "port must be between 1 and 65535".into(),
                ));
            }
            return Url::parse(&format!("http://127.0.0.1:{port}"))
                .map(Self)
                .map_err(|error| AppError::InvalidTarget(error.to_string()));
        }

        let url = Url::parse(input).map_err(|_| {
            AppError::InvalidTarget(
                "use a local port or an HTTP URL, for example `gnar 3000`".into(),
            )
        })?;

        if !matches!(url.scheme(), "http" | "https") || url.host().is_none() {
            return Err(AppError::InvalidTarget(
                "target must use http:// or https://".into(),
            ));
        }

        Ok(Self(url))
    }

    fn as_str(&self) -> &str {
        self.0.as_str()
    }
}

fn probe_reason(error: &reqwest::Error) -> String {
    if error.is_timeout() {
        return format!("did not respond within {}s", PROBE_TIMEOUT.as_secs());
    }
    if error.is_connect() {
        return "connection refused or the service is not listening".into();
    }
    error.to_string()
}

#[derive(Debug)]
pub enum AppError {
    NoLocalService,
    Discovery(std::io::Error),
    InvalidTarget(String),
    Unavailable,
    Output(std::io::Error),
    Edge(String),
}

impl fmt::Display for AppError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::NoLocalService => write!(
                formatter,
                "no local HTTP service found; start one or provide a port, for example `gnar 3000`"
            ),
            Self::Discovery(error) => {
                write!(
                    formatter,
                    "could not inspect the current directory: {error}"
                )
            }
            Self::InvalidTarget(reason) => write!(formatter, "invalid target: {reason}"),
            Self::Unavailable => write!(
                formatter,
                "start the local service or choose another target, then try again"
            ),
            Self::Output(error) => write!(formatter, "could not write output: {error}"),
            Self::Edge(reason) => write!(formatter, "edge connection failed: {reason}"),
        }
    }
}

impl std::error::Error for AppError {}

impl From<std::io::Error> for AppError {
    fn from(error: std::io::Error) -> Self {
        Self::Output(error)
    }
}

#[cfg(test)]
mod tests {
    use super::{AppError, Target};

    #[test]
    fn port_becomes_loopback_http_url() {
        let target = Target::parse("5173").unwrap();

        assert_eq!(target.as_str(), "http://127.0.0.1:5173/");
    }

    #[test]
    fn url_keeps_path_and_query() {
        let target = Target::parse("http://localhost:3000/api?mode=dev").unwrap();

        assert_eq!(target.as_str(), "http://localhost:3000/api?mode=dev");
    }

    #[test]
    fn zero_is_not_a_port() {
        assert!(matches!(
            Target::parse("0"),
            Err(AppError::InvalidTarget(_))
        ));
    }

    #[test]
    fn non_http_url_is_rejected() {
        assert!(matches!(
            Target::parse("tcp://localhost:3000"),
            Err(AppError::InvalidTarget(_))
        ));
    }
}
