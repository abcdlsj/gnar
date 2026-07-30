use std::net::TcpListener as StdTcpListener;
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{Duration, SystemTime};

use axum::Router;
use axum::extract::Request;
use tokio::net::TcpListener;

struct ChildGuard(Child);

impl Drop for ChildGuard {
    fn drop(&mut self) {
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}

struct Edge {
    url: String,
    database: PathBuf,
    config_dir: PathBuf,
    _process: ChildGuard,
}

impl Edge {
    fn cleanup(&self) {
        let _ = std::fs::remove_file(&self.database);
        let _ = std::fs::remove_dir_all(&self.config_dir);
    }
}

const APPROVAL_SECRET: &str = "let-me-in";

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn an_unknown_token_is_refused_rather_than_treated_as_anonymous() {
    let edge = start_edge(&[]).await;
    let upstream = start_upstream().await;

    let rejected = run_client(&edge, &upstream, &[], Some("bogus-token"));

    assert!(
        rejected.contains("does not recognize the stored token"),
        "{rejected}"
    );
    edge.cleanup();
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn a_device_code_becomes_a_token_only_after_approval() {
    let edge = start_edge(&[]).await;
    let client = reqwest::Client::new();

    let start = start_device_flow(&client, &edge).await;
    let user_code = start["user_code"].as_str().unwrap().to_string();
    let device_code = start["device_code"].as_str().unwrap().to_string();
    assert_eq!(
        start["verification_uri"].as_str().unwrap(),
        format!("{}/device", edge.url)
    );
    assert_eq!(
        redeem(&client, &edge, &device_code).await["status"],
        "pending"
    );

    let approved = approve(&client, &edge, &user_code, "alice", APPROVAL_SECRET).await;
    assert!(approved.status().is_success());

    let granted = redeem(&client, &edge, &device_code).await;
    assert_eq!(granted["status"], "approved");
    assert_eq!(granted["account"], "alice");
    assert!(!granted["token"].as_str().unwrap().is_empty());

    assert_eq!(
        redeem(&client, &edge, &device_code).await["status"],
        "expired",
        "a redeemed device code must not yield its token again"
    );
    edge.cleanup();
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn approval_requires_the_secret() {
    let edge = start_edge(&[]).await;
    let client = reqwest::Client::new();
    let start = start_device_flow(&client, &edge).await;
    let user_code = start["user_code"].as_str().unwrap().to_string();

    let refused = approve(&client, &edge, &user_code, "alice", "wrong").await;

    assert_eq!(refused.status(), 403);
    assert_eq!(
        redeem(&client, &edge, start["device_code"].as_str().unwrap()).await["status"],
        "pending",
        "a failed approval must leave the code unapproved"
    );
    edge.cleanup();
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn a_token_reports_its_account_and_quota() {
    let edge = start_edge(&["--account-tunnels", "1"]).await;
    let client = reqwest::Client::new();
    let token = mint_account(&client, &edge, "alice").await;

    let account: serde_json::Value = client
        .get(format!("{}/v1/account", edge.url))
        .bearer_auth(&token)
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();

    assert_eq!(account["account"], "alice");
    assert_eq!(account["tunnels"], 1);
    edge.cleanup();
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn an_authenticated_name_is_reserved_and_stops_expiring() {
    let edge = start_edge(&[]).await;
    let upstream = start_upstream().await;
    let client = reqwest::Client::new();
    let token = mint_account(&client, &edge, "alice").await;

    let output = run_client(&edge, &upstream, &["--name", "checkout"], Some(&token));

    assert!(output.contains("reserved by alice"), "{output}");
    let connection = rusqlite::Connection::open(&edge.database).unwrap();
    let (kind, never_expires): (String, bool) = connection
        .query_row(
            "SELECT kind, expires_at IS NULL FROM endpoints WHERE name = 'checkout'",
            [],
            |row| Ok((row.get(0)?, row.get(1)?)),
        )
        .unwrap();
    assert_eq!(kind, "reserved");
    assert!(never_expires);

    drop(connection);
    edge.cleanup();
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn a_reserved_name_is_refused_to_everyone_else() {
    let edge = start_edge(&[]).await;
    let upstream = start_upstream().await;
    let client = reqwest::Client::new();
    let alice = mint_account(&client, &edge, "alice").await;
    let bob = mint_account(&client, &edge, "bob").await;
    run_client(&edge, &upstream, &["--name", "checkout"], Some(&alice));

    let refused = run_client(&edge, &upstream, &["--name", "checkout"], Some(&bob));
    assert!(refused.contains("reserved by alice"), "{refused}");

    let anonymous = run_client(&edge, &upstream, &["--name", "checkout"], None);
    assert!(anonymous.contains("requires an account"), "{anonymous}");
    edge.cleanup();
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn only_the_owner_can_release_a_reserved_name() {
    let edge = start_edge(&[]).await;
    let upstream = start_upstream().await;
    let client = reqwest::Client::new();
    let alice = mint_account(&client, &edge, "alice").await;
    let bob = mint_account(&client, &edge, "bob").await;
    run_client(&edge, &upstream, &["--name", "checkout"], Some(&alice));

    assert_eq!(release(&client, &edge, "checkout", &bob).await, 404);
    assert_eq!(release(&client, &edge, "checkout", &alice).await, 200);

    let now_bobs = run_client(&edge, &upstream, &["--name", "checkout"], Some(&bob));
    assert!(now_bobs.contains("reserved by bob"), "{now_bobs}");
    edge.cleanup();
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn traffic_beyond_the_rate_limit_is_rejected_with_a_reason() {
    let edge = start_anonymous_edge(&["--anonymous-requests", "2"]).await;
    let upstream = start_upstream().await;
    let client = reqwest::Client::new();
    let _tunnel = spawn_client(&edge, &upstream, &["--name", "public-demo"], None);
    let public = format!("{}/t/public-demo/", edge.url);
    wait_for_success(&client, &public).await;

    let mut statuses = Vec::new();
    for _ in 0..4 {
        statuses.push(client.get(&public).send().await.unwrap().status().as_u16());
    }

    assert!(
        statuses.contains(&429),
        "a 2/min limit must reject extra requests, got {statuses:?}"
    );
    let limited = client.get(&public).send().await.unwrap();
    assert_eq!(limited.status(), 429);
    assert!(limited.text().await.unwrap().contains("Rate limit reached"));
    edge.cleanup();
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn anonymous_tunnel_limit_is_enforced() {
    let edge = start_anonymous_edge(&["--anonymous-tunnels", "1"]).await;
    let upstream = start_upstream().await;
    let client = reqwest::Client::new();
    let _first = spawn_client(&edge, &upstream, &["--name", "first"], None);
    wait_for_success(&client, &format!("{}/t/first/", edge.url)).await;

    let second = run_client(&edge, &upstream, &["--name", "second"], None);

    assert!(
        second.contains("already has 1 anonymous tunnels"),
        "{second}"
    );
    edge.cleanup();
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn device_code_creation_is_rate_limited() {
    let edge = start_edge(&[]).await;
    let client = reqwest::Client::new();
    let mut statuses = Vec::new();
    for _ in 0..7 {
        statuses.push(
            client
                .post(format!("{}/v1/device/code", edge.url))
                .send()
                .await
                .unwrap()
                .status(),
        );
    }

    assert_eq!(
        statuses.last(),
        Some(&reqwest::StatusCode::TOO_MANY_REQUESTS)
    );
    edge.cleanup();
}

async fn redeem(client: &reqwest::Client, edge: &Edge, device_code: &str) -> serde_json::Value {
    client
        .post(format!("{}/v1/device/token", edge.url))
        .json(&serde_json::json!({ "device_code": device_code }))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap()
}

async fn start_device_flow(client: &reqwest::Client, edge: &Edge) -> serde_json::Value {
    client
        .post(format!("{}/v1/device/code", edge.url))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap()
}

async fn approve(
    client: &reqwest::Client,
    edge: &Edge,
    user_code: &str,
    account: &str,
    secret: &str,
) -> reqwest::Response {
    client
        .post(format!("{}/device", edge.url))
        .form(&[
            ("user_code", user_code),
            ("account", account),
            ("secret", secret),
            ("action", "approve"),
        ])
        .send()
        .await
        .unwrap()
}

async fn mint_account(client: &reqwest::Client, edge: &Edge, name: &str) -> String {
    let start = start_device_flow(client, edge).await;
    approve(
        client,
        edge,
        start["user_code"].as_str().unwrap(),
        name,
        APPROVAL_SECRET,
    )
    .await;
    redeem(client, edge, start["device_code"].as_str().unwrap()).await["token"]
        .as_str()
        .unwrap()
        .to_string()
}

async fn release(client: &reqwest::Client, edge: &Edge, name: &str, token: &str) -> u16 {
    client
        .post(format!("{}/v1/endpoints/release?name={name}", edge.url))
        .bearer_auth(token)
        .send()
        .await
        .unwrap()
        .status()
        .as_u16()
}

async fn start_edge(extra: &[&str]) -> Edge {
    start_edge_in_mode(extra, true).await
}

async fn start_anonymous_edge(extra: &[&str]) -> Edge {
    start_edge_in_mode(extra, false).await
}

async fn start_edge_in_mode(extra: &[&str], accounts: bool) -> Edge {
    let port = free_port();
    let url = format!("http://127.0.0.1:{port}");
    let database = temp_path("db");
    let config_dir = temp_path("config");
    let mut command = Command::new(env!("CARGO_BIN_EXE_gnar"));
    command.args([
        "serve",
        "--listen",
        &format!("127.0.0.1:{port}"),
        "--public-url",
        &url,
        "--database",
        database.to_str().unwrap(),
    ]);
    if accounts {
        command.args(["--approval-secret", APPROVAL_SECRET]);
    } else {
        command.arg("--anonymous-only");
    }
    command
        .args(extra)
        .stdout(Stdio::null())
        .stderr(Stdio::null());
    let process = ChildGuard(command.spawn().unwrap());
    let client = reqwest::Client::new();
    for _ in 0..200 {
        if let Ok(response) = client.get(format!("{url}/healthz")).send().await
            && response.status().is_success()
        {
            return Edge {
                url,
                database,
                config_dir,
                _process: process,
            };
        }
        tokio::time::sleep(Duration::from_millis(25)).await;
    }
    panic!("edge did not become healthy");
}

struct Upstream {
    address: std::net::SocketAddr,
    task: tokio::task::JoinHandle<()>,
}

impl Drop for Upstream {
    fn drop(&mut self) {
        self.task.abort();
    }
}

async fn start_upstream() -> Upstream {
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let address = listener.local_addr().unwrap();
    let app = Router::new()
        .fallback(|request: Request| async move { format!("ok {}", request.uri().path()) });
    let task = tokio::spawn(async move {
        let _ = axum::serve(listener, app).await;
    });
    Upstream { address, task }
}

fn client_command(edge: &Edge, upstream: &Upstream, args: &[&str], token: Option<&str>) -> Command {
    std::fs::create_dir_all(&edge.config_dir).unwrap();
    let credentials = match token {
        Some(token) => serde_json::json!({ "edges": { &edge.url: token } }),
        None => serde_json::json!({ "edges": {} }),
    };
    std::fs::write(
        edge.config_dir.join("credentials.json"),
        serde_json::to_string(&credentials).unwrap(),
    )
    .unwrap();
    let mut command = Command::new(env!("CARGO_BIN_EXE_gnar"));
    command
        .arg(format!("http://{}", upstream.address))
        .args(["--edge", &edge.url, "--no-tui"])
        .args(args)
        .env("GNAR_CONFIG_DIR", &edge.config_dir);
    command
}

fn run_client(edge: &Edge, upstream: &Upstream, args: &[&str], token: Option<&str>) -> String {
    let mut command = client_command(edge, upstream, args, token);
    let mut child = command
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap();
    std::thread::sleep(Duration::from_millis(900));
    let _ = child.kill();
    let output = child.wait_with_output().unwrap();
    format!(
        "{}{}",
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    )
}

fn spawn_client(
    edge: &Edge,
    upstream: &Upstream,
    args: &[&str],
    token: Option<&str>,
) -> ChildGuard {
    let mut command = client_command(edge, upstream, args, token);
    ChildGuard(
        command
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .unwrap(),
    )
}

async fn wait_for_success(client: &reqwest::Client, url: &str) {
    for _ in 0..200 {
        if let Ok(response) = client.get(url).send().await
            && response.status().is_success()
        {
            return;
        }
        tokio::time::sleep(Duration::from_millis(25)).await;
    }
    panic!("{url} did not become available");
}

fn free_port() -> u16 {
    StdTcpListener::bind("127.0.0.1:0")
        .unwrap()
        .local_addr()
        .unwrap()
        .port()
}

fn temp_path(label: &str) -> PathBuf {
    static NEXT: AtomicU64 = AtomicU64::new(0);

    let nonce = SystemTime::now()
        .duration_since(SystemTime::UNIX_EPOCH)
        .unwrap()
        .as_nanos();
    let unique = NEXT.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "gnar-{label}-{}-{nonce}-{unique}",
        std::process::id()
    ))
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn a_non_interactive_edge_serves_anonymous_tunnels_without_asking() {
    let port = free_port();
    let database = temp_path("db");
    let mut child = Command::new(env!("CARGO_BIN_EXE_gnar"))
        .args([
            "serve",
            "--listen",
            &format!("127.0.0.1:{port}"),
            "--database",
            database.to_str().unwrap(),
        ])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap();

    let client = reqwest::Client::new();
    let mut healthy = false;
    for _ in 0..200 {
        if let Ok(response) = client
            .get(format!("http://127.0.0.1:{port}/healthz"))
            .send()
            .await
            && response.status().is_success()
        {
            healthy = true;
            break;
        }
        tokio::time::sleep(Duration::from_millis(25)).await;
    }
    assert!(healthy, "a non-interactive edge must start unattended");

    let start = client
        .post(format!("http://127.0.0.1:{port}/v1/device/code"))
        .send()
        .await
        .unwrap();
    assert_eq!(
        start.status(),
        404,
        "an anonymous-only edge must not issue device codes"
    );

    let approved = client
        .post(format!("http://127.0.0.1:{port}/device"))
        .form(&[
            ("user_code", "AAAA-BBBB"),
            ("account", "mallory"),
            ("action", "approve"),
        ])
        .send()
        .await
        .unwrap();
    assert_eq!(
        approved.status(),
        404,
        "an anonymous-only edge must not create accounts"
    );

    let anonymous = client
        .get(format!("http://127.0.0.1:{port}/t/nobody"))
        .send()
        .await
        .unwrap();
    assert_eq!(anonymous.status(), 502, "tunnel routing stays available");

    let _ = child.kill();
    let _ = child.wait();
    let _ = std::fs::remove_file(&database);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn a_public_bind_requires_explicit_opt_in() {
    let port = free_port();
    let database = temp_path("db");
    let output = Command::new(env!("CARGO_BIN_EXE_gnar"))
        .args([
            "serve",
            "--listen",
            &format!("0.0.0.0:{port}"),
            "--database",
            database.to_str().unwrap(),
        ])
        .output()
        .unwrap();

    assert!(!output.status.success());
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(stderr.contains("--allow-public-bind"), "{stderr}");
    let _ = std::fs::remove_file(&database);
}
