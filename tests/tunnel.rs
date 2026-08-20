use std::net::TcpListener as StdTcpListener;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::time::{Duration, SystemTime};

use axum::Router;
use axum::body::to_bytes;
use axum::extract::Request;
use axum::extract::ws::{Message as AxumMessage, WebSocketUpgrade};
use axum::routing::get;
use futures_util::{SinkExt, StreamExt};
use tokio::net::TcpListener;
use tokio_tungstenite::tungstenite::Message;
use tokio_tungstenite::{MaybeTlsStream, WebSocketStream};

type WsStream = WebSocketStream<MaybeTlsStream<tokio::net::TcpStream>>;
static TEST_LOCK: tokio::sync::Mutex<()> = tokio::sync::Mutex::const_new(());

struct ChildGuard(Child);

impl Drop for ChildGuard {
    fn drop(&mut self) {
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn request_crosses_edge_and_is_recorded() {
    let _guard = TEST_LOCK.lock().await;
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
    assert_eq!(missing.status(), 404);
    assert!(missing.text().await.unwrap().contains("Tunnel not found"));

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

    let slashless = reqwest::Client::builder()
        .redirect(reqwest::redirect::Policy::none())
        .build()
        .unwrap()
        .get(format!("{edge_url}/t/integration"))
        .send()
        .await
        .unwrap();
    assert_eq!(slashless.status(), 301);
    assert_eq!(
        slashless
            .headers()
            .get("location")
            .unwrap()
            .to_str()
            .unwrap(),
        format!("/t/integration/")
    );

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

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn websocket_frames_cross_the_edge_in_both_directions() {
    let _guard = TEST_LOCK.lock().await;
    let upstream = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let upstream_address = upstream.local_addr().unwrap();
    let upstream_app = Router::new().route(
        "/ws",
        get(|ws: WebSocketUpgrade| async move {
            ws.on_upgrade(|mut socket| async move {
                while let Some(Ok(message)) = socket.recv().await {
                    if matches!(message, AxumMessage::Text(_) | AxumMessage::Binary(_))
                        && socket.send(message).await.is_err()
                    {
                        break;
                    }
                }
            })
        }),
    );
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

    let target = format!("http://{upstream_address}");
    let tunnel = Command::new(binary)
        .args([&target, "--edge", &edge_url, "--name", "integration"])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let mut tunnel = ChildGuard(tunnel);

    let public_url = format!("ws://127.0.0.1:{edge_port}/t/integration/ws");
    let mut socket = wait_for_ws(&public_url).await;
    socket
        .send(Message::text("hello through gnar"))
        .await
        .unwrap();
    let reply = socket.next().await.unwrap().unwrap();
    assert_eq!(reply, Message::text("hello through gnar"));
    socket.send(Message::binary(vec![1, 2, 3])).await.unwrap();
    let reply = socket.next().await.unwrap().unwrap();
    assert_eq!(reply, Message::binary(vec![1, 2, 3]));
    let oversized_send = socket
        .send(Message::binary(vec![0; 4 * 1024 * 1024 + 1]))
        .await;
    if oversized_send.is_ok() {
        let oversized = tokio::time::timeout(Duration::from_secs(5), socket.next())
            .await
            .unwrap();
        assert!(
            matches!(oversized, None | Some(Err(_)) | Some(Ok(Message::Close(_)))),
            "oversized WebSocket frames must be rejected, got {oversized:?}"
        );
    }
    let _ = socket.close(None).await;

    tunnel.0.kill().unwrap();
    edge.0.kill().unwrap();
    upstream_task.abort();
    let _ = std::fs::remove_file(database);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 8)]
async fn websocket_burst_stays_relayable_with_bounded_connections() {
    let _guard = TEST_LOCK.lock().await;
    let upstream = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let upstream_address = upstream.local_addr().unwrap();
    let upstream_app = Router::new().route(
        "/ws",
        get(|ws: WebSocketUpgrade| async move {
            ws.on_upgrade(|mut socket| async move {
                while let Some(Ok(message)) = socket.recv().await {
                    if matches!(message, AxumMessage::Text(_) | AxumMessage::Binary(_))
                        && socket.send(message).await.is_err()
                    {
                        break;
                    }
                }
            })
        }),
    );
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
            "--websocket-concurrent",
            "32",
            "--websocket-bytes-per-minute-mib",
            "64",
        ])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let mut edge = ChildGuard(edge);
    wait_for_status(&format!("{edge_url}/healthz"), 200).await;

    let target = format!("http://{upstream_address}");
    let tunnel = Command::new(binary)
        .args([&target, "--edge", &edge_url, "--name", "burst"])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let mut tunnel = ChildGuard(tunnel);
    let public_url = format!("ws://127.0.0.1:{edge_port}/t/burst/ws");
    let mut workers = Vec::new();
    for connection in 0..32u8 {
        let public_url = public_url.clone();
        workers.push(tokio::spawn(async move {
            let mut socket = wait_for_ws(&public_url).await;
            for message in 0..64u8 {
                let payload = vec![connection ^ message; 4096];
                socket
                    .send(Message::Binary(payload.clone().into()))
                    .await
                    .unwrap();
                assert_eq!(
                    socket.next().await.unwrap().unwrap(),
                    Message::Binary(payload.into())
                );
            }
            socket.close(None).await.unwrap();
        }));
    }
    for worker in workers {
        worker.await.unwrap();
    }

    tunnel.0.kill().unwrap();
    edge.0.kill().unwrap();
    upstream_task.abort();
    let _ = std::fs::remove_file(database);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn websocket_traffic_budget_closes_a_bursty_connection() {
    let _guard = TEST_LOCK.lock().await;
    let upstream = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let upstream_address = upstream.local_addr().unwrap();
    let upstream_app = Router::new().route(
        "/ws",
        get(|ws: WebSocketUpgrade| async move {
            ws.on_upgrade(|mut socket| async move {
                while let Some(Ok(message)) = socket.recv().await {
                    if matches!(message, AxumMessage::Text(_) | AxumMessage::Binary(_))
                        && socket.send(message).await.is_err()
                    {
                        break;
                    }
                }
            })
        }),
    );
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
            "--websocket-bytes-per-minute-mib",
            "1",
        ])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let mut edge = ChildGuard(edge);
    wait_for_status(&format!("{edge_url}/healthz"), 200).await;

    let target = format!("http://{upstream_address}");
    let tunnel = Command::new(binary)
        .args([&target, "--edge", &edge_url, "--name", "budget"])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let mut tunnel = ChildGuard(tunnel);

    let public_url = format!("ws://127.0.0.1:{edge_port}/t/budget/ws");
    let mut socket = wait_for_ws(&public_url).await;
    socket
        .send(Message::Binary(vec![7; 1024 * 1024 + 1].into()))
        .await
        .unwrap();
    let close_code = tokio::time::timeout(Duration::from_secs(5), async {
        loop {
            match socket.next().await {
                Some(Ok(Message::Close(Some(frame)))) => break Some(frame.code.into()),
                Some(Ok(Message::Close(None))) | None | Some(Err(_)) => break None,
                Some(Ok(_)) => {}
            }
        }
    })
    .await
    .unwrap();
    assert_eq!(close_code, Some(1008));

    tunnel.0.kill().unwrap();
    edge.0.kill().unwrap();
    upstream_task.abort();
    let _ = std::fs::remove_file(database);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn websocket_concurrency_limit_rejects_the_next_exchange() {
    let _guard = TEST_LOCK.lock().await;
    let upstream = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let upstream_address = upstream.local_addr().unwrap();
    let upstream_app = Router::new().route(
        "/ws",
        get(|ws: WebSocketUpgrade| async move {
            ws.on_upgrade(|mut socket| async move {
                while let Some(Ok(message)) = socket.recv().await {
                    if matches!(message, AxumMessage::Text(_) | AxumMessage::Binary(_))
                        && socket.send(message).await.is_err()
                    {
                        break;
                    }
                }
            })
        }),
    );
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
            "--websocket-concurrent",
            "1",
        ])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let mut edge = ChildGuard(edge);
    wait_for_status(&format!("{edge_url}/healthz"), 200).await;

    let target = format!("http://{upstream_address}");
    let tunnel = Command::new(binary)
        .args([&target, "--edge", &edge_url, "--name", "concurrency"])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let mut tunnel = ChildGuard(tunnel);

    let public_url = format!("ws://127.0.0.1:{edge_port}/t/concurrency/ws");
    let mut first = wait_for_ws(&public_url).await;
    first.send(Message::text("held")).await.unwrap();
    assert_eq!(first.next().await.unwrap().unwrap(), Message::text("held"));
    let (mut second, _) = tokio_tungstenite::connect_async(&public_url).await.unwrap();
    let close_code = tokio::time::timeout(Duration::from_secs(5), second.next())
        .await
        .unwrap()
        .and_then(|message| message.ok())
        .and_then(|message| match message {
            Message::Close(Some(frame)) => Some(frame.code.into()),
            _ => None,
        });
    assert_eq!(close_code, Some(1013));

    let _ = first.close(None).await;
    let _ = second.close(None).await;
    tunnel.0.kill().unwrap();
    edge.0.kill().unwrap();
    upstream_task.abort();
    let _ = std::fs::remove_file(database);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn websocket_idle_timeout_closes_a_silent_client() {
    let _guard = TEST_LOCK.lock().await;
    let upstream = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let upstream_address = upstream.local_addr().unwrap();
    let upstream_app = Router::new().route(
        "/ws",
        get(|ws: WebSocketUpgrade| async move {
            ws.on_upgrade(|mut socket| async move {
                while let Some(Ok(message)) = socket.recv().await {
                    if matches!(message, AxumMessage::Text(_) | AxumMessage::Binary(_))
                        && socket.send(message).await.is_err()
                    {
                        break;
                    }
                }
            })
        }),
    );
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
            "--websocket-idle-timeout-secs",
            "1",
        ])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let mut edge = ChildGuard(edge);
    wait_for_status(&format!("{edge_url}/healthz"), 200).await;

    let target = format!("http://{upstream_address}");
    let tunnel = Command::new(binary)
        .args([&target, "--edge", &edge_url, "--name", "idle"])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let mut tunnel = ChildGuard(tunnel);

    let public_url = format!("ws://127.0.0.1:{edge_port}/t/idle/ws");
    let mut socket = wait_for_ws(&public_url).await;
    tokio::time::sleep(Duration::from_secs(2)).await;
    let close_code = tokio::time::timeout(Duration::from_secs(5), async {
        loop {
            match socket.next().await {
                Some(Ok(Message::Close(Some(frame)))) => break Some(frame.code.into()),
                Some(Ok(_)) => {}
                Some(Err(_)) | None => break None,
            }
        }
    })
    .await
    .unwrap();
    assert_eq!(close_code, Some(1001));

    tunnel.0.kill().unwrap();
    edge.0.kill().unwrap();
    upstream_task.abort();
    let _ = std::fs::remove_file(database);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn host_header_is_preserved_by_default() {
    let _guard = TEST_LOCK.lock().await;
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

    let upstream = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let upstream_address = upstream.local_addr().unwrap();
    let upstream_app = Router::new().fallback(|request: Request| async move {
        let host = request
            .headers()
            .get("host")
            .and_then(|value| value.to_str().ok())
            .unwrap_or("")
            .to_string();
        format!("host={host}")
    });
    let upstream_task = tokio::spawn(axum::serve(upstream, upstream_app).into_future());

    let target = format!("http://{upstream_address}");
    let tunnel = Command::new(binary)
        .args([&target, "--edge", &edge_url, "--name", "integration"])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let mut tunnel = ChildGuard(tunnel);

    wait_for_body(&format!("{edge_url}/t/integration/ready")).await;
    let response = reqwest::Client::new()
        .get(format!("{edge_url}/t/integration/host"))
        .header("Host", "public.example.test")
        .send()
        .await
        .unwrap();
    assert_eq!(response.text().await.unwrap(), "host=public.example.test");

    tunnel.0.kill().unwrap();
    edge.0.kill().unwrap();
    upstream_task.abort();
    let _ = std::fs::remove_file(database);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn host_header_rewrites_when_disabled() {
    let _guard = TEST_LOCK.lock().await;
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

    let upstream = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let upstream_address = upstream.local_addr().unwrap();
    let upstream_app = Router::new().fallback(|request: Request| async move {
        let host = request
            .headers()
            .get("host")
            .and_then(|value| value.to_str().ok())
            .unwrap_or("")
            .to_string();
        format!("host={host}")
    });
    let upstream_task = tokio::spawn(axum::serve(upstream, upstream_app).into_future());

    let target = format!("http://{upstream_address}");
    let tunnel = Command::new(binary)
        .args([
            &target,
            "--edge",
            &edge_url,
            "--name",
            "integration",
            "--preserve-host",
            "false",
        ])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let mut tunnel = ChildGuard(tunnel);

    let public_url = format!("{edge_url}/t/integration/host");
    let body = wait_for_body(&public_url).await;
    assert!(
        body.contains(&format!("127.0.0.1:{}", upstream_address.port())),
        "expected the rewritten target host, got {body}"
    );

    tunnel.0.kill().unwrap();
    edge.0.kill().unwrap();
    upstream_task.abort();
    let _ = std::fs::remove_file(database);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn websocket_upgrade_is_rejected_when_disabled() {
    let _guard = TEST_LOCK.lock().await;
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

    let upstream = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let upstream_address = upstream.local_addr().unwrap();
    let upstream_task = tokio::spawn(axum::serve(upstream, Router::new()).into_future());

    let target = format!("http://{upstream_address}");
    let tunnel = Command::new(binary)
        .args([
            &target,
            "--edge",
            &edge_url,
            "--name",
            "integration",
            "--websocket",
            "false",
        ])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let mut tunnel = ChildGuard(tunnel);

    let public_url = format!("ws://127.0.0.1:{edge_port}/t/integration/ws");
    for _ in 0..100 {
        if tokio_tungstenite::connect_async(&public_url).await.is_ok() {
            panic!("WebSocket upgrade succeeded while forwarding was disabled");
        }
        tokio::time::sleep(Duration::from_millis(25)).await;
    }

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

async fn wait_for_ws(url: &str) -> WsStream {
    for _ in 0..100 {
        if let Ok((socket, _)) = tokio_tungstenite::connect_async(url).await {
            return socket;
        }
        tokio::time::sleep(Duration::from_millis(25)).await;
    }
    panic!("{url} did not accept a WebSocket upgrade");
}
