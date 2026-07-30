use std::net::TcpListener as StdTcpListener;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::time::{Duration, SystemTime};

use axum::Router;
use axum::body::to_bytes;
use axum::extract::Request;
use tokio::net::TcpListener;

struct ChildGuard(Child);

impl Drop for ChildGuard {
    fn drop(&mut self) {
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn request_crosses_edge_and_is_recorded() {
    let upstream = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let upstream_address = upstream.local_addr().unwrap();
    let upstream_app = Router::new().fallback(|request: Request| async move {
        let method = request.method().clone();
        let uri = request.uri().clone();
        let body = to_bytes(request.into_body(), 64 * 1024).await.unwrap();
        format!("{method} {uri} {}", String::from_utf8_lossy(&body))
    });
    let upstream_task = tokio::spawn(axum::serve(upstream, upstream_app).into_future());

    let edge_port = free_port();
    let edge_url = format!("http://127.0.0.1:{edge_port}");
    let database = temp_database();
    let binary = env!("CARGO_BIN_EXE_gnar");
    let edge = Command::new(binary)
        .args([
            "serve",
            "--listen",
            &format!("127.0.0.1:{edge_port}"),
            "--public-url",
            &edge_url,
            "--database",
            database.to_str().unwrap(),
        ])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let mut edge = ChildGuard(edge);
    wait_for_status(&format!("{edge_url}/healthz"), 200).await;
    let missing = reqwest::get(format!("{edge_url}/t/missing")).await.unwrap();
    assert_eq!(missing.status(), 502);
    assert!(missing.text().await.unwrap().contains("Tunnel offline"));

    let target = format!("http://{upstream_address}/base");
    let tunnel = Command::new(binary)
        .args([&target, "--edge", &edge_url, "--name", "integration"])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let mut tunnel = ChildGuard(tunnel);

    let public_url = format!("{edge_url}/t/integration/hello?x=1");
    let response = wait_for_body(&public_url).await;
    assert_eq!(response, "GET /base/hello?x=1 ");

    let response = reqwest::Client::new()
        .post(format!("{edge_url}/t/integration/webhook"))
        .body("payload")
        .send()
        .await
        .unwrap();
    assert_eq!(response.text().await.unwrap(), "POST /base/webhook payload");

    wait_for_count(&database, "SELECT count(*) FROM request_metrics", 2).await;
    edge.0.kill().unwrap();
    edge.0.wait().unwrap();
    let restarted = Command::new(binary)
        .args([
            "serve",
            "--listen",
            &format!("127.0.0.1:{edge_port}"),
            "--public-url",
            &edge_url,
            "--database",
            database.to_str().unwrap(),
        ])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    edge = ChildGuard(restarted);
    wait_for_status(&format!("{edge_url}/healthz"), 200).await;
    let response = wait_for_body(&format!("{edge_url}/t/integration/reconnected")).await;
    assert_eq!(response, "GET /base/reconnected ");

    wait_for_count(&database, "SELECT count(*) FROM request_metrics", 3).await;
    assert_eq!(
        count_now(
            &database,
            "SELECT count(*) FROM endpoints WHERE name = 'integration'"
        ),
        1
    );
    assert_eq!(
        count_now(&database, "SELECT count(*) FROM tunnel_sessions"),
        2
    );
    assert_eq!(
        count_now(&database, "SELECT count(*) FROM schema_migrations"),
        3
    );

    tunnel.0.kill().unwrap();
    edge.0.kill().unwrap();
    upstream_task.abort();
    let _ = std::fs::remove_file(database);
}

fn free_port() -> u16 {
    let listener = StdTcpListener::bind("127.0.0.1:0").unwrap();
    listener.local_addr().unwrap().port()
}

fn temp_database() -> PathBuf {
    let nonce = SystemTime::now()
        .duration_since(SystemTime::UNIX_EPOCH)
        .unwrap()
        .as_nanos();
    std::env::temp_dir().join(format!("gnar-{nonce}.db"))
}

async fn wait_for_status(url: &str, expected: u16) {
    let client = reqwest::Client::new();
    for _ in 0..100 {
        if let Ok(response) = client.get(url).send().await
            && response.status().as_u16() == expected
        {
            return;
        }
        tokio::time::sleep(Duration::from_millis(25)).await;
    }
    panic!("{url} did not return HTTP {expected}");
}

fn count_now(database: &Path, query: &str) -> i64 {
    let connection = rusqlite::Connection::open(database).unwrap();
    connection.query_row(query, [], |row| row.get(0)).unwrap()
}

async fn wait_for_count(database: &Path, query: &str, expected: i64) {
    let mut latest = 0;
    for _ in 0..100 {
        latest = count_now(database, query);
        if latest == expected {
            return;
        }
        tokio::time::sleep(Duration::from_millis(25)).await;
    }
    panic!("`{query}` returned {latest}, expected {expected}");
}

async fn wait_for_body(url: &str) -> String {
    let client = reqwest::Client::new();
    for _ in 0..100 {
        if let Ok(response) = client.get(url).send().await
            && response.status().is_success()
        {
            return response.text().await.unwrap();
        }
        tokio::time::sleep(Duration::from_millis(25)).await;
    }
    panic!("{url} did not become available");
}
