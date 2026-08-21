use std::collections::BTreeMap;
use std::fs::{self, OpenOptions};
use std::io::{self, BufRead, Write};
use std::path::PathBuf;
use std::time::Duration;

use reqwest::Client;
use serde::{Deserialize, Serialize};

use crate::app::AppError;
use crate::output::{Event, Output};

const POLL_TIMEOUT: Duration = Duration::from_secs(600);
const MAX_ENROLLMENT_KEY_BYTES: usize = 4096;

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
        .post(edge_endpoint(edge, "/v1/device/code")?)
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
            .post(edge_endpoint(edge, "/v1/device/token")?)
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

pub async fn enroll(edge: &str, account: &str, output: &Output) -> Result<(), AppError> {
    let account = normalize_account(account)?;
    output.event(Event::EnrollmentStarted { account: &account })?;
    let enrollment_key = read_secret_line()?;
    let client = Client::builder()
        .timeout(Duration::from_secs(15))
        .build()
        .map_err(|error| AppError::Edge(error.to_string()))?;
    let endpoint = edge_endpoint(edge, "/v1/device/enroll")?;
    let response = client
        .post(endpoint)
        .json(&EnrollmentRequest {
            account: &account,
            enrollment_key: &enrollment_key,
        })
        .send()
        .await
        .map_err(|error| unreachable_edge(edge, &error))?;
    let status = response.status();
    if !status.is_success() {
        return Err(enrollment_error(status, response).await);
    }

    let reply: EnrollmentResponse = response.json().await.map_err(|_| {
        AppError::Edge("edge sent an unexpected enrollment reply; try again".into())
    })?;
    if reply.status != "enrolled" || reply.token.is_empty() || reply.account.is_empty() {
        return Err(AppError::Edge(
            "edge sent an incomplete enrollment reply; try again".into(),
        ));
    }
    store_token(edge, &reply.token)?;
    output.event(Event::EnrollmentSucceeded {
        account: &reply.account,
    })?;
    Ok(())
}

pub async fn enroll_with_invite(
    edge: &str,
    invite_key: &str,
    output: &Output,
) -> Result<(), AppError> {
    output.event(Event::InviteEnrollmentStarted)?;
    let client = Client::builder()
        .timeout(Duration::from_secs(15))
        .build()
        .map_err(|error| AppError::Edge(error.to_string()))?;
    let endpoint = edge_endpoint(edge, "/v1/device/enroll")?;
    let response = client
        .post(endpoint)
        .json(&InviteRequest { invite_key })
        .send()
        .await
        .map_err(|error| unreachable_edge(edge, &error))?;
    let status = response.status();
    if !status.is_success() {
        return Err(enrollment_error(status, response).await);
    }

    let reply: EnrollmentResponse = response.json().await.map_err(|_| {
        AppError::Edge("edge sent an unexpected enrollment reply; try again".into())
    })?;
    if reply.status != "enrolled" || reply.token.is_empty() || reply.account.is_empty() {
        return Err(AppError::Edge(
            "edge sent an incomplete enrollment reply; try again".into(),
        ));
    }
    store_token(edge, &reply.token)?;
    output.event(Event::EnrollmentSucceeded {
        account: &reply.account,
    })?;
    Ok(())
}

pub async fn enroll_with_invite_stdin(edge: &str, output: &Output) -> Result<(), AppError> {
    let invite_key = read_secret_line()?;
    enroll_with_invite(edge, &invite_key, output).await
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
            "{}?name={name}",
            edge_endpoint(edge, "/v1/endpoints/release")?
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
        .get(edge_endpoint(edge, "/v1/account")?)
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

fn edge_endpoint(edge: &str, suffix: &str) -> Result<String, AppError> {
    let mut url = url::Url::parse(edge)
        .map_err(|error| AppError::Edge(format!("invalid edge URL: {error}")))?;
    let base = url.path().trim_end_matches('/');
    let path = if base.is_empty() {
        suffix.to_string()
    } else {
        format!("{base}{suffix}")
    };
    url.set_path(&path);
    url.set_query(None);
    url.set_fragment(None);
    Ok(url.to_string())
}

fn normalize_account(account: &str) -> Result<String, AppError> {
    let normalized = account.trim().to_ascii_lowercase();
    if crate::protocol::valid_name(&normalized) {
        return Ok(normalized);
    }
    Err(AppError::Edge(format!(
        "--account must be 1 to {} lowercase letters, numbers, or hyphens",
        crate::protocol::MAX_NAME_LENGTH
    )))
}

fn read_secret_line() -> Result<String, AppError> {
    let mut stdin = io::stdin().lock();
    read_enrollment_key_from(&mut stdin)
}

fn read_enrollment_key_from(reader: &mut impl BufRead) -> Result<String, AppError> {
    let mut input = String::new();
    let mut bounded = std::io::Read::take(reader, (MAX_ENROLLMENT_KEY_BYTES + 1) as u64);
    bounded.read_line(&mut input).map_err(|error| {
        AppError::Edge(format!(
            "could not read the enrollment key from stdin: {error}"
        ))
    })?;
    if input.len() > MAX_ENROLLMENT_KEY_BYTES {
        return Err(AppError::Edge(
            "the enrollment key from stdin is too long".into(),
        ));
    }
    let key = input.strip_suffix('\n').unwrap_or(&input);
    let key = key.strip_suffix('\r').unwrap_or(key);
    let key = key.trim();
    if key.is_empty() {
        return Err(AppError::Edge(
            "secret stdin must contain one non-empty line".into(),
        ));
    }
    Ok(key.to_string())
}

#[derive(Serialize)]
struct EnrollmentRequest<'a> {
    account: &'a str,
    enrollment_key: &'a str,
}

#[derive(Serialize)]
struct InviteRequest<'a> {
    invite_key: &'a str,
}

#[derive(Deserialize)]
struct EnrollmentResponse {
    status: String,
    account: String,
    token: String,
}

#[derive(Deserialize)]
struct EnrollmentErrorResponse {
    code: Option<String>,
}

async fn enrollment_error(status: reqwest::StatusCode, response: reqwest::Response) -> AppError {
    let body = response.bytes().await.unwrap_or_default();
    let code = serde_json::from_slice::<EnrollmentErrorResponse>(&body)
        .ok()
        .and_then(|error| error.code);
    let reason = match (status, code.as_deref()) {
        (reqwest::StatusCode::NOT_FOUND, Some("enrollment_disabled")) => {
            "enrollment is disabled on this edge; ask its operator to enable accounts"
        }
        (reqwest::StatusCode::FORBIDDEN, Some("invalid_enrollment_key")) => {
            "the enrollment key was rejected; check the operator-provided key and try again"
        }
        (reqwest::StatusCode::FORBIDDEN, Some("invalid_invite_key")) => {
            "the invite key was not recognized; check the key and try again"
        }
        (reqwest::StatusCode::FORBIDDEN, Some("invite_key_expired")) => {
            "the invite key has expired; ask the edge operator for a new key"
        }
        (reqwest::StatusCode::FORBIDDEN, Some("invite_key_exhausted")) => {
            "the invite key has reached its usage limit; ask the edge operator for a new key"
        }
        (reqwest::StatusCode::BAD_REQUEST, Some("malformed_account")) => {
            "the account name is invalid; use 1 to 48 lowercase letters, numbers, or hyphens"
        }
        (reqwest::StatusCode::TOO_MANY_REQUESTS, Some("rate_limited")) => {
            "too many enrollment attempts; wait and try again"
        }
        (reqwest::StatusCode::SERVICE_UNAVAILABLE, Some("edge_unavailable")) => {
            "the edge is unavailable; check its health and try again"
        }
        (reqwest::StatusCode::NOT_FOUND, _) => {
            "this edge does not provide enrollment; check the edge URL and its configuration"
        }
        (reqwest::StatusCode::TOO_MANY_REQUESTS, _) => {
            "too many enrollment attempts; wait and try again"
        }
        (reqwest::StatusCode::SERVICE_UNAVAILABLE, _) => {
            "the edge is unavailable; check its health and try again"
        }
        _ => "the edge rejected enrollment; check its configuration and try again",
    };
    AppError::Edge(format!("{reason} (HTTP {})", status.as_u16()))
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
    let temporary = path.with_extension(format!(
        "json.tmp-{}-{}",
        std::process::id(),
        rand::random::<u64>()
    ));
    let result = write_private(&temporary, content.as_bytes()).and_then(|_| {
        fs::rename(&temporary, &path).map_err(|error| {
            AppError::Edge(format!("could not replace {}: {error}", path.display()))
        })
    });
    if result.is_err() {
        let _ = fs::remove_file(&temporary);
    }
    result
}

#[cfg(unix)]
fn write_private(path: &std::path::Path, content: &[u8]) -> Result<(), AppError> {
    use std::os::unix::fs::OpenOptionsExt;

    let mut file = OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(path)
        .map_err(|error| AppError::Edge(format!("could not create {}: {error}", path.display())))?;
    file.write_all(content)
        .and_then(|_| file.sync_all())
        .map_err(|error| AppError::Edge(format!("could not write {}: {error}", path.display())))
}

#[cfg(not(unix))]
fn write_private(path: &std::path::Path, content: &[u8]) -> Result<(), AppError> {
    let mut file = OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(path)
        .map_err(|error| AppError::Edge(format!("could not create {}: {error}", path.display())))?;
    file.write_all(content)
        .and_then(|_| file.sync_all())
        .map_err(|error| AppError::Edge(format!("could not write {}: {error}", path.display())))
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
    use std::io::{BufRead, Cursor};

    use super::{Credentials, key, read_enrollment_key_from};

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

    #[test]
    fn enrollment_key_is_read_once_and_accepts_one_trailing_newline() {
        let mut reader = Cursor::new("  let-me-in  \nunused\n");
        assert_eq!(read_enrollment_key_from(&mut reader).unwrap(), "let-me-in");
        let mut remaining = String::new();
        reader.read_line(&mut remaining).unwrap();
        assert_eq!(remaining, "unused\n");
    }

    #[test]
    fn enrollment_key_rejects_oversized_input_without_echoing_it() {
        let secret = "x".repeat(super::MAX_ENROLLMENT_KEY_BYTES + 1);
        let mut reader = Cursor::new(secret.clone());
        let error = read_enrollment_key_from(&mut reader)
            .unwrap_err()
            .to_string();
        assert!(error.contains("too long"));
        assert!(!error.contains(&secret));
    }
}
