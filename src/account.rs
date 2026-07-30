use std::collections::BTreeMap;
use std::fs;
use std::io::{self, Write};
use std::path::PathBuf;
use std::time::Duration;

use reqwest::Client;
use serde::{Deserialize, Serialize};

use crate::app::AppError;

const POLL_TIMEOUT: Duration = Duration::from_secs(600);

#[derive(Default, Deserialize, Serialize)]
struct Credentials {
    #[serde(default)]
    edges: BTreeMap<String, String>,
}

pub fn token_for(edge: &str) -> Option<String> {
    let credentials = read().ok()?;
    credentials.edges.get(&key(edge)).cloned()
}

pub fn signed_in_edges() -> Vec<String> {
    read()
        .map(|credentials| credentials.edges.into_keys().collect())
        .unwrap_or_default()
}

pub fn command_edge(explicit: Option<&str>) -> Result<String, AppError> {
    if let Some(edge) = explicit {
        return Ok(edge.to_string());
    }
    let mut edges = signed_in_edges();
    match edges.len() {
        1 => Ok(edges.remove(0)),
        0 => Err(AppError::Edge(
            "no edge server is available; self-host one with `gnar serve`, then pass its URL with --edge"
                .into(),
        )),
        _ => Err(AppError::Edge(
            "more than one edge is signed in; choose one with --edge or GNAR_EDGE".into(),
        )),
    }
}

pub async fn login(edge: &str) -> Result<(), AppError> {
    let client = Client::builder()
        .timeout(Duration::from_secs(15))
        .build()
        .map_err(|error| AppError::Edge(error.to_string()))?;

    let start: DeviceCodeResponse = client
        .post(format!("{}/v1/device/code", edge.trim_end_matches('/')))
        .send()
        .await
        .and_then(reqwest::Response::error_for_status)
        .map_err(|error| unreachable_edge(edge, &error))?
        .json()
        .await
        .map_err(|error| AppError::Edge(format!("edge sent an unexpected login reply: {error}")))?;

    println!(
        "Open {} and enter code  {}",
        start.verification_uri, start.user_code
    );
    print!("Waiting for approval…");
    let _ = io::stdout().flush();

    let interval = Duration::from_secs(start.interval.clamp(1, 10));
    let deadline = tokio::time::Instant::now() + POLL_TIMEOUT;
    loop {
        if tokio::time::Instant::now() >= deadline {
            println!();
            return Err(AppError::Edge(
                "the login code expired; run `gnar login` again".into(),
            ));
        }
        tokio::time::sleep(interval).await;

        let poll: TokenResponse = client
            .post(format!("{}/v1/device/token", edge.trim_end_matches('/')))
            .json(&serde_json::json!({ "device_code": start.device_code }))
            .send()
            .await
            .and_then(reqwest::Response::error_for_status)
            .map_err(|error| unreachable_edge(edge, &error))?
            .json()
            .await
            .map_err(|error| {
                AppError::Edge(format!("edge sent an unexpected login reply: {error}"))
            })?;

        match poll.status.as_str() {
            "pending" => {
                print!(".");
                let _ = io::stdout().flush();
            }
            "approved" => {
                let token = poll.token.unwrap_or_default();
                let account = poll.account.unwrap_or_else(|| "unknown".into());
                store_token(edge, &token)?;
                println!("\n✓ Signed in as {account}");
                return Ok(());
            }
            "denied" => {
                println!();
                return Err(AppError::Edge("the login request was denied".into()));
            }
            _ => {
                println!();
                return Err(AppError::Edge(
                    "the login code expired; run `gnar login` again".into(),
                ));
            }
        }
    }
}

pub async fn release(edge: &str, name: &str) -> Result<(), AppError> {
    let Some(token) = token_for(edge) else {
        return Err(AppError::Edge(format!(
            "not signed in to {edge}; run `gnar login` first"
        )));
    };
    let client = Client::builder()
        .timeout(Duration::from_secs(10))
        .build()
        .map_err(|error| AppError::Edge(error.to_string()))?;
    let response = client
        .post(format!(
            "{}/v1/endpoints/release?name={name}",
            edge.trim_end_matches('/')
        ))
        .bearer_auth(&token)
        .send()
        .await
        .map_err(|error| unreachable_edge(edge, &error))?;

    if response.status().is_success() {
        println!("Released {name}");
        return Ok(());
    }
    if response.status() == reqwest::StatusCode::NOT_FOUND {
        return Err(AppError::Edge(format!(
            "{name} is not reserved by your account"
        )));
    }
    Err(AppError::Edge(format!(
        "could not release {name}: the edge returned HTTP {}",
        response.status().as_u16()
    )))
}

pub fn logout(edge: &str) -> Result<(), AppError> {
    let mut credentials = read()?;
    match credentials.edges.remove(&key(edge)) {
        Some(_) => {
            write(&credentials)?;
            println!("Signed out of {edge}");
        }
        None => println!("Not signed in to {edge}"),
    }
    Ok(())
}

pub async fn whoami(edge: &str) -> Result<(), AppError> {
    let Some(token) = token_for(edge) else {
        println!("Not signed in to {edge}");
        return Ok(());
    };

    let client = Client::builder()
        .timeout(Duration::from_secs(10))
        .build()
        .map_err(|error| AppError::Edge(error.to_string()))?;
    let response: AccountResponse = client
        .get(format!("{}/v1/account", edge.trim_end_matches('/')))
        .bearer_auth(&token)
        .send()
        .await
        .and_then(reqwest::Response::error_for_status)
        .map_err(|error| unreachable_edge(edge, &error))?
        .json()
        .await
        .map_err(|error| AppError::Edge(format!("edge sent an unexpected reply: {error}")))?;

    println!(
        "Signed in to {edge} as {}\n  {} tunnels · {} requests/min",
        response.account, response.tunnels, response.requests_per_minute
    );
    Ok(())
}

fn store_token(edge: &str, token: &str) -> Result<(), AppError> {
    let mut credentials = read()?;
    credentials.edges.insert(key(edge), token.to_string());
    write(&credentials)
}

fn key(edge: &str) -> String {
    edge.trim_end_matches('/').to_string()
}

fn path() -> Result<PathBuf, AppError> {
    let base = std::env::var_os("GNAR_CONFIG_DIR")
        .map(PathBuf::from)
        .or_else(|| {
            std::env::var_os("XDG_CONFIG_HOME")
                .map(PathBuf::from)
                .map(|dir| dir.join("gnar"))
        })
        .or_else(|| {
            std::env::var_os("HOME")
                .map(PathBuf::from)
                .map(|home| home.join(".config").join("gnar"))
        })
        .ok_or_else(|| {
            AppError::Edge("could not locate a configuration directory for credentials".into())
        })?;
    Ok(base.join("credentials.json"))
}

fn read() -> Result<Credentials, AppError> {
    let path = path()?;
    match fs::read_to_string(&path) {
        Ok(content) => Ok(serde_json::from_str(&content).unwrap_or_default()),
        Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(Credentials::default()),
        Err(error) => Err(AppError::Edge(format!(
            "could not read {}: {error}",
            path.display()
        ))),
    }
}

fn write(credentials: &Credentials) -> Result<(), AppError> {
    let path = path()?;
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).map_err(|error| {
            AppError::Edge(format!("could not create {}: {error}", parent.display()))
        })?;
    }
    let content = serde_json::to_string_pretty(credentials)
        .map_err(|error| AppError::Edge(error.to_string()))?;
    fs::write(&path, content)
        .map_err(|error| AppError::Edge(format!("could not write {}: {error}", path.display())))?;
    restrict(&path)
}

#[cfg(unix)]
fn restrict(path: &std::path::Path) -> Result<(), AppError> {
    use std::os::unix::fs::PermissionsExt;

    fs::set_permissions(path, fs::Permissions::from_mode(0o600)).map_err(|error| {
        AppError::Edge(format!(
            "could not restrict {} to this user: {error}",
            path.display()
        ))
    })
}

#[cfg(not(unix))]
fn restrict(_path: &std::path::Path) -> Result<(), AppError> {
    Ok(())
}

fn unreachable_edge(edge: &str, error: &reqwest::Error) -> AppError {
    if error.status() == Some(reqwest::StatusCode::NOT_FOUND) {
        return AppError::Edge(format!(
            "{edge} serves anonymous tunnels only, so it has no accounts; \
             its operator can enable them by restarting it with an approval secret"
        ));
    }
    AppError::Edge(format!(
        "could not reach {edge}: check that an edge is running there and reachable from this machine"
    ))
}

#[derive(Deserialize)]
struct DeviceCodeResponse {
    device_code: String,
    user_code: String,
    verification_uri: String,
    #[serde(default = "default_interval")]
    interval: u64,
}

fn default_interval() -> u64 {
    2
}

#[derive(Deserialize)]
struct TokenResponse {
    status: String,
    token: Option<String>,
    account: Option<String>,
}

#[derive(Deserialize)]
struct AccountResponse {
    account: String,
    tunnels: usize,
    requests_per_minute: u32,
}

#[cfg(test)]
mod tests {
    use super::{Credentials, key};

    #[test]
    fn edge_key_ignores_a_trailing_slash() {
        assert_eq!(
            key("https://gnar.example.com/"),
            key("https://gnar.example.com")
        );
    }

    #[test]
    fn credentials_keep_one_token_per_edge() {
        let mut credentials = Credentials::default();
        credentials
            .edges
            .insert(key("https://gnar.example.com"), "remote".into());
        credentials
            .edges
            .insert(key("http://127.0.0.1:8910/"), "local".into());

        let encoded = serde_json::to_string(&credentials).unwrap();
        let decoded: Credentials = serde_json::from_str(&encoded).unwrap();

        assert_eq!(decoded.edges["https://gnar.example.com"], "remote");
        assert_eq!(decoded.edges["http://127.0.0.1:8910"], "local");
        assert_eq!(
            decoded.edges.into_keys().collect::<Vec<_>>(),
            ["http://127.0.0.1:8910", "https://gnar.example.com"]
        );
    }

    #[test]
    fn invalid_credentials_do_not_create_available_edges() {
        let decoded: Credentials = serde_json::from_str("not json").unwrap_or_default();

        assert!(decoded.edges.is_empty());
    }
}
