use std::process::Command;
use std::thread;

use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpListener;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn explicit_target_is_probed_before_edge_connection() {
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let address = listener.local_addr().unwrap();

    tokio::spawn(async move {
        let (mut stream, _) = listener.accept().await.unwrap();
        let mut request = [0; 1024];
        let bytes_read = stream.read(&mut request).await.unwrap();
        assert!(bytes_read > 0);
        stream
            .write_all(b"HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
            .await
            .unwrap();
    });

    let binary = env!("CARGO_BIN_EXE_gnar");
    let target = format!("http://{address}");
    let output = thread::spawn(move || Command::new(binary).arg(target).output().unwrap())
        .join()
        .unwrap();

    assert!(!output.status.success());
    let stdout = String::from_utf8(output.stdout).unwrap();
    assert!(stdout.contains("HTTP 204"), "{stdout}");
}

async fn edge_failure(extra: &[&str], signed_in: &[&str]) -> String {
    // A reachable local target, so the run gets past the probe and fails at the edge.
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let address = listener.local_addr().unwrap();
    let serving = tokio::spawn(async move {
        while let Ok((mut stream, _)) = listener.accept().await {
            let mut request = [0; 1024];
            let _ = stream.read(&mut request).await;
            let _ = stream
                .write_all(b"HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
                .await;
        }
    });

    let mut args = vec![format!("http://{address}"), "--no-tui".to_string()];
    args.extend(extra.iter().map(ToString::to_string));
    let config_dir = std::env::temp_dir().join(format!("gnar-probe-{}", address.port()));
    std::fs::create_dir_all(&config_dir).unwrap();
    let edges = signed_in
        .iter()
        .map(|edge| {
            (
                (*edge).to_string(),
                serde_json::Value::String("token".into()),
            )
        })
        .collect::<serde_json::Map<_, _>>();
    std::fs::write(
        config_dir.join("credentials.json"),
        serde_json::to_vec(&serde_json::json!({ "edges": edges })).unwrap(),
    )
    .unwrap();
    let command_config_dir = config_dir.clone();
    let output = tokio::task::spawn_blocking(move || {
        Command::new(env!("CARGO_BIN_EXE_gnar"))
            .args(&args)
            .env_remove("GNAR_EDGE")
            .env("GNAR_CONFIG_DIR", command_config_dir)
            .output()
            .unwrap()
    })
    .await
    .unwrap();

    serving.abort();
    let _ = std::fs::remove_dir_all(config_dir);
    String::from_utf8_lossy(&output.stderr).into_owned()
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn no_available_edge_explains_how_to_self_host() {
    let stderr = edge_failure(&[], &[]).await;

    assert!(stderr.contains("no edge server is available"), "{stderr}");
    assert!(stderr.contains("gnar serve"), "{stderr}");
    assert!(!stderr.contains("gnar.dev"), "{stderr}");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn one_signed_in_edge_is_selected_automatically() {
    let stderr = edge_failure(&[], &["http://127.0.0.1:1"]).await;

    assert!(stderr.contains("http://127.0.0.1:1"), "{stderr}");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn several_edges_require_an_explicit_choice_without_a_terminal() {
    let stderr = edge_failure(&[], &["http://127.0.0.1:1", "http://127.0.0.1:2"]).await;

    assert!(
        stderr.contains("more than one edge is signed in"),
        "{stderr}"
    );
    assert!(stderr.contains("--edge"), "{stderr}");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn a_bare_host_and_port_is_accepted_as_an_edge() {
    let stderr = edge_failure(&["--edge", "127.0.0.1:1"], &[]).await;

    assert!(
        stderr.contains("http://127.0.0.1:1"),
        "a bare host:port should be read as http://: {stderr}"
    );
    assert!(!stderr.contains("builder error"), "{stderr}");
}

#[test]
fn a_non_http_edge_is_rejected_before_connecting() {
    let output = Command::new(env!("CARGO_BIN_EXE_gnar"))
        .args(["9", "--edge", "ws://127.0.0.1:8910"])
        .output()
        .unwrap();

    assert!(!output.status.success());
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(stderr.contains("must use http:// or https://"), "{stderr}");
}

#[test]
fn json_mode_emits_structured_error() {
    let output = Command::new(env!("CARGO_BIN_EXE_gnar"))
        .args(["0", "--json"])
        .output()
        .unwrap();

    assert!(!output.status.success());
    let event: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(event["type"], "error");
    assert!(
        event["message"]
            .as_str()
            .unwrap()
            .contains("invalid target")
    );
}
