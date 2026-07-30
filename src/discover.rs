use std::collections::HashSet;
use std::fs;
use std::path::Path;
use std::time::Duration;

use reqwest::Client;
use reqwest::header::HeaderMap;
use serde_json::Value;
use tokio::task::JoinSet;
use url::Url;

use crate::app::AppError;

const DISCOVERY_TIMEOUT: Duration = Duration::from_millis(450);
const COMMON_PORTS: [u16; 8] = [3000, 5173, 8000, 8080, 4200, 5000, 3001, 8888];
const MAX_FINGERPRINT_BYTES: usize = 16 * 1024;
const MAX_DETAIL_CHARS: usize = 48;

#[derive(Debug)]
pub struct LocalService {
    pub url: Url,
    pub kind: String,
    pub detail: Option<String>,
    pub status: u16,
}

#[derive(Clone, Debug, Eq, PartialEq)]
struct Candidate {
    port: u16,
    hint: Option<String>,
    process: Option<String>,
    rank: usize,
}

struct Probe {
    status: u16,
    server: Option<String>,
    powered_by: Option<String>,
    body: String,
}

pub async fn local_services(client: &Client) -> Result<Vec<LocalService>, AppError> {
    let current_dir = std::env::current_dir().map_err(AppError::Discovery)?;
    let candidates = candidates(&current_dir, listening_ports().await);
    let services = find(client, candidates).await;
    if services.is_empty() {
        return Err(AppError::NoLocalService);
    }
    Ok(services)
}

async fn find(client: &Client, candidates: Vec<Candidate>) -> Vec<LocalService> {
    let mut probes = JoinSet::new();

    for candidate in candidates {
        let client = client.clone();
        probes.spawn(async move {
            let url = Url::parse(&format!("http://127.0.0.1:{}/", candidate.port)).unwrap();
            let probe = tokio::time::timeout(DISCOVERY_TIMEOUT, probe(&client, &url)).await;
            match probe {
                Ok(Some(probe)) => Some((candidate, url, probe)),
                _ => None,
            }
        });
    }

    let mut found = Vec::new();
    while let Some(result) = probes.join_next().await {
        if let Ok(Some(probe)) = result {
            found.push(probe);
        }
    }

    let mut ranked = found
        .into_iter()
        .filter_map(|(candidate, url, probe)| {
            let identity = identify(&candidate, &probe)?;
            Some((
                (
                    !identity.serves_content,
                    candidate.rank + identity.rank,
                    url.port(),
                ),
                LocalService {
                    url,
                    kind: identity.kind,
                    detail: identity.detail,
                    status: probe.status,
                },
            ))
        })
        .collect::<Vec<_>>();
    ranked.sort_by_key(|(rank, _)| *rank);

    ranked.into_iter().map(|(_, service)| service).collect()
}

async fn probe(client: &Client, url: &Url) -> Option<Probe> {
    let mut response = client.get(url.clone()).send().await.ok()?;
    let status = response.status().as_u16();
    let server = header(response.headers(), "server");
    let powered_by = header(response.headers(), "x-powered-by");

    let mut body = Vec::new();
    while body.len() < MAX_FINGERPRINT_BYTES {
        match response.chunk().await {
            Ok(Some(chunk)) => body.extend_from_slice(&chunk),
            _ => break,
        }
    }
    body.truncate(MAX_FINGERPRINT_BYTES);

    Some(Probe {
        status,
        server,
        powered_by,
        body: String::from_utf8_lossy(&body).into_owned(),
    })
}

fn header(headers: &HeaderMap, name: &str) -> Option<String> {
    headers
        .get(name)?
        .to_str()
        .ok()
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
}

struct Identity {
    kind: String,
    detail: Option<String>,
    serves_content: bool,
    rank: usize,
}

fn identify(candidate: &Candidate, probe: &Probe) -> Option<Identity> {
    if is_own_process(candidate) || is_system_service(candidate, probe) {
        return None;
    }

    let framework = framework(probe).or_else(|| candidate.hint.clone().map(|kind| (kind, 4)));
    let (kind, rank) = match framework {
        Some((kind, rank)) => (kind, rank),
        None => (runtime(candidate, probe), 8),
    };

    Some(Identity {
        kind,
        detail: page_title(&probe.body).or_else(|| candidate.process.clone()),
        serves_content: (200..400).contains(&probe.status) && !probe.body.trim().is_empty(),
        rank,
    })
}

fn framework(probe: &Probe) -> Option<(String, usize)> {
    let body = &probe.body;
    let signatures: [(&str, &str); 12] = [
        ("__NEXT_DATA__", "Next.js"),
        ("/_next/static", "Next.js"),
        ("/@vite/client", "Vite"),
        ("/@react-refresh", "Vite"),
        ("__vite_plugin_react", "Vite"),
        ("__NUXT__", "Nuxt"),
        ("__svelte", "SvelteKit"),
        ("__remixContext", "Remix"),
        ("ng-version", "Angular"),
        ("data-turbo", "Rails"),
        ("csrfmiddlewaretoken", "Django"),
        ("Ollama is running", "Ollama"),
    ];
    for (needle, kind) in signatures {
        if body.contains(needle) {
            return Some((kind.into(), 0));
        }
    }

    let server = probe.server.as_deref().unwrap_or_default();
    let servers: [(&str, &str); 8] = [
        ("gunicorn", "Gunicorn"),
        ("uvicorn", "Uvicorn"),
        ("hypercorn", "Hypercorn"),
        ("werkzeug", "Flask"),
        ("waitress", "Waitress"),
        ("puma", "Rails"),
        ("nginx", "nginx"),
        ("caddy", "Caddy"),
    ];
    for (needle, kind) in servers {
        if server.to_ascii_lowercase().contains(needle) {
            return Some((kind.into(), 2));
        }
    }

    if probe
        .powered_by
        .as_deref()
        .is_some_and(|value| value.eq_ignore_ascii_case("Express"))
    {
        return Some(("Express".into(), 2));
    }

    None
}

fn runtime(candidate: &Candidate, probe: &Probe) -> String {
    if let Some(server) = &probe.server {
        return truncate(server);
    }
    if let Some(shape) = body_shape(&probe.body) {
        return shape.into();
    }
    match &candidate.process {
        Some(process) => truncate(process),
        None => "HTTP service".into(),
    }
}

fn body_shape(body: &str) -> Option<&'static str> {
    let head = body.trim_start();
    if head.starts_with('{') || head.starts_with('[') {
        return Some("JSON API");
    }
    let lowered = head.to_ascii_lowercase();
    if lowered.starts_with("<!doctype html") || lowered.contains("<html") {
        return Some("web app");
    }
    None
}

fn page_title(body: &str) -> Option<String> {
    let start = body.to_ascii_lowercase().find("<title")?;
    let after = body[start..].find('>')? + start + 1;
    let end = body[after..].to_ascii_lowercase().find("</title")? + after;
    let title = body[after..end]
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ");
    (!title.is_empty()).then(|| truncate(&title))
}

fn truncate(value: &str) -> String {
    if value.chars().count() <= MAX_DETAIL_CHARS {
        return value.to_string();
    }
    value.chars().take(MAX_DETAIL_CHARS - 1).collect::<String>() + "…"
}

fn is_own_process(candidate: &Candidate) -> bool {
    candidate
        .process
        .as_deref()
        .is_some_and(|process| process.eq_ignore_ascii_case("gnar"))
}

fn is_system_service(candidate: &Candidate, probe: &Probe) -> bool {
    const SYSTEM_SERVERS: [&str; 2] = ["airtunes", "airplay"];
    const SYSTEM_PROCESSES: [&str; 4] = ["controlcenter", "rapportd", "sharingd", "remoted"];

    if let Some(server) = &probe.server {
        let server = server.to_ascii_lowercase();
        if SYSTEM_SERVERS.iter().any(|name| server.contains(name)) {
            return true;
        }
    }
    candidate.process.as_deref().is_some_and(|process| {
        let process = process.to_ascii_lowercase().replace(' ', "");
        SYSTEM_PROCESSES.iter().any(|name| process == *name)
    })
}

fn candidates(root: &Path, listening: Vec<Listener>) -> Vec<Candidate> {
    let mut candidates = project_candidates(root);
    let mut ports = candidates
        .iter()
        .map(|candidate| candidate.port)
        .collect::<HashSet<_>>();

    for listener in listening {
        if ports.insert(listener.port) {
            candidates.push(Candidate {
                port: listener.port,
                hint: None,
                process: Some(listener.process),
                rank: 50,
            });
        } else if let Some(candidate) = candidates
            .iter_mut()
            .find(|candidate| candidate.port == listener.port)
        {
            candidate.process = Some(listener.process);
        }
    }

    for port in COMMON_PORTS {
        if ports.insert(port) {
            candidates.push(Candidate {
                port,
                hint: None,
                process: None,
                rank: 100,
            });
        }
    }

    candidates
}

struct Listener {
    port: u16,
    process: String,
}

async fn listening_ports() -> Vec<Listener> {
    let mut command = tokio::process::Command::new("lsof");
    command.args(["-nP", "-iTCP", "-sTCP:LISTEN", "-Fcn"]);
    if let Ok(user) = std::env::var("USER") {
        command.args(["-a", "-u", &user]);
    }
    command.kill_on_drop(true);
    let output = tokio::time::timeout(Duration::from_millis(300), command.output()).await;
    let Ok(Ok(output)) = output else {
        return Vec::new();
    };
    parse_listeners(&String::from_utf8_lossy(&output.stdout))
}

fn parse_listeners(output: &str) -> Vec<Listener> {
    let mut listeners = Vec::new();
    let mut seen = HashSet::new();
    let mut process = String::new();

    for line in output.lines() {
        if let Some(name) = line.strip_prefix('c') {
            process = name.to_string();
        } else if let Some(address) = line.strip_prefix('n') {
            let Some((_, port)) = address.rsplit_once(':') else {
                continue;
            };
            let Ok(port) = port.parse::<u16>() else {
                continue;
            };
            if seen.insert(port) {
                listeners.push(Listener {
                    port,
                    process: process.clone(),
                });
            }
        }
    }

    listeners.sort_by_key(|listener| listener.port);
    listeners
}

fn project_candidates(root: &Path) -> Vec<Candidate> {
    let mut candidates = Vec::new();
    let mut add = |port: u16, hint: &str| {
        candidates.push(Candidate {
            port,
            hint: Some(hint.to_string()),
            process: None,
            rank: 0,
        });
    };

    if let Some(package) = read_json(&root.join("package.json")) {
        let framework = [
            ("next", "Next.js", 3000),
            ("nuxt", "Nuxt", 3000),
            ("@remix-run/dev", "Remix", 3000),
            ("@sveltejs/kit", "SvelteKit", 5173),
            ("@angular/core", "Angular", 4200),
            ("vite", "Vite", 5173),
        ]
        .into_iter()
        .find(|(name, _, _)| has_package(&package, name));

        if let Some((_, hint, default_port)) = framework {
            add(package_port(&package).unwrap_or(default_port), hint);
        }
    }

    if root.join("manage.py").is_file() {
        add(8000, "Django");
    }
    if root.join("Gemfile").is_file() {
        add(3000, "Rails");
    }

    candidates
}

fn read_json(path: &Path) -> Option<Value> {
    let content = fs::read_to_string(path).ok()?;
    serde_json::from_str(&content).ok()
}

fn has_package(package: &Value, name: &str) -> bool {
    ["dependencies", "devDependencies"]
        .iter()
        .filter_map(|field| package.get(field))
        .any(|dependencies| dependencies.get(name).is_some())
}

fn package_port(package: &Value) -> Option<u16> {
    let scripts = package.get("scripts")?.as_object()?;
    ["dev", "start", "serve"]
        .iter()
        .filter_map(|name| scripts.get(*name)?.as_str())
        .find_map(command_port)
}

fn command_port(command: &str) -> Option<u16> {
    let mut words = command.split_whitespace();
    while let Some(word) = words.next() {
        if matches!(word, "--port" | "-p") {
            return words.next()?.parse().ok().filter(|port| *port > 0);
        }
        if let Some(port) = word.strip_prefix("--port=") {
            return port.parse().ok().filter(|port| *port > 0);
        }
    }
    None
}

#[cfg(test)]
mod tests {
    use std::fs;

    use reqwest::Client;
    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    use tokio::net::TcpListener;

    use super::{
        Candidate, Probe, command_port, find, identify, page_title, parse_listeners,
        project_candidates,
    };

    fn probe(server: Option<&str>, body: &str) -> Probe {
        Probe {
            status: 200,
            server: server.map(str::to_string),
            powered_by: None,
            body: body.into(),
        }
    }

    fn candidate(process: Option<&str>) -> Candidate {
        Candidate {
            port: 3000,
            hint: None,
            process: process.map(str::to_string),
            rank: 50,
        }
    }

    #[tokio::test]
    #[ignore = "inspects this machine's real listeners"]
    async fn machine_report() {
        let listeners = super::listening_ports().await;
        println!("listeners:");
        for listener in &listeners {
            println!("  {:>6}  {}", listener.port, listener.process);
        }
        let candidates = super::candidates(&std::env::current_dir().unwrap(), listeners);
        println!("discovered:");
        for service in super::find(&Client::new(), candidates).await {
            println!(
                "  {:<14} {:<28} {} {}",
                service.kind,
                service.url.as_str(),
                service.status,
                service.detail.unwrap_or_default()
            );
        }
    }

    #[test]
    fn reads_port_from_development_command() {
        assert_eq!(command_port("vite --host --port 4310"), Some(4310));
        assert_eq!(command_port("next dev --port=3200"), Some(3200));
    }

    #[test]
    fn reads_listener_ports_with_owning_process() {
        let listeners =
            parse_listeners("p1\ncnode\nn127.0.0.1:3000\np2\ncpython3\nn*:8000\nn[::1]:3000\n");

        let named = listeners
            .iter()
            .map(|listener| (listener.port, listener.process.as_str()))
            .collect::<Vec<_>>();
        assert_eq!(named, vec![(3000, "node"), (8000, "python3")]);
    }

    #[test]
    fn recognizes_project_frameworks() {
        let root = std::env::temp_dir().join(format!("gnar-discover-{}", std::process::id()));
        fs::create_dir_all(&root).unwrap();
        fs::write(
            root.join("package.json"),
            r#"{"devDependencies":{"vite":"latest"},"scripts":{"dev":"vite --port 4310"}}"#,
        )
        .unwrap();

        let candidates = project_candidates(&root);

        fs::remove_file(root.join("package.json")).unwrap();
        fs::remove_dir(&root).unwrap();
        assert_eq!(candidates[0].port, 4310);
        assert_eq!(candidates[0].hint.as_deref(), Some("Vite"));
    }

    #[test]
    fn body_signatures_name_the_framework() {
        for (body, expected) in [
            (r#"<script id="__NEXT_DATA__">{}</script>"#, "Next.js"),
            (r#"<script src="/@vite/client"></script>"#, "Vite"),
            ("window.__NUXT__ = {}", "Nuxt"),
            ("Ollama is running", "Ollama"),
        ] {
            let identity = identify(&candidate(None), &probe(None, body)).unwrap();
            assert_eq!(identity.kind, expected, "{body}");
        }
    }

    #[test]
    fn server_header_names_the_runtime() {
        let identity = identify(&candidate(None), &probe(Some("gunicorn/21.2.0"), "")).unwrap();
        assert_eq!(identity.kind, "Gunicorn");

        let identity = identify(&candidate(None), &probe(Some("Werkzeug/3.0.1"), "")).unwrap();
        assert_eq!(identity.kind, "Flask");
    }

    #[test]
    fn page_title_becomes_the_detail() {
        let body = "<html><head><title>  Infer Lab ·\n  Transformer </title></head>";

        assert_eq!(page_title(body).unwrap(), "Infer Lab · Transformer");

        let identity = identify(&candidate(Some("node")), &probe(None, body)).unwrap();
        assert_eq!(identity.detail.as_deref(), Some("Infer Lab · Transformer"));
    }

    #[test]
    fn falls_back_to_body_shape_then_process() {
        let identity = identify(&candidate(None), &probe(None, r#"{"ok":true}"#)).unwrap();
        assert_eq!(identity.kind, "JSON API");

        let identity = identify(&candidate(None), &probe(None, "<html></html>")).unwrap();
        assert_eq!(identity.kind, "web app");

        let identity = identify(&candidate(Some("python3")), &probe(None, "pong")).unwrap();
        assert_eq!(identity.kind, "python3");

        let identity = identify(&candidate(None), &probe(None, "pong")).unwrap();
        assert_eq!(identity.kind, "HTTP service");
    }

    #[test]
    fn system_services_and_gnar_itself_are_hidden() {
        assert!(identify(&candidate(None), &probe(Some("AirTunes/940.23.1"), "")).is_none());
        assert!(identify(&candidate(Some("ControlCenter")), &probe(None, "")).is_none());
        assert!(identify(&candidate(Some("gnar")), &probe(None, "<html>")).is_none());
    }

    #[tokio::test]
    async fn identifies_a_reachable_service() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let port = listener.local_addr().unwrap().port();
        tokio::spawn(async move {
            let (mut stream, _) = listener.accept().await.unwrap();
            let mut request = [0; 1024];
            let bytes_read = stream.read(&mut request).await.unwrap();
            assert!(bytes_read > 0);
            let body = "<html><head><title>Checkout</title></head></html>";
            stream
                .write_all(
                    format!(
                        "HTTP/1.1 200 OK\r\nX-Powered-By: Express\r\nContent-Length: {}\r\n\r\n{body}",
                        body.len()
                    )
                    .as_bytes(),
                )
                .await
                .unwrap();
        });

        let service = find(
            &Client::new(),
            vec![Candidate {
                port,
                hint: None,
                process: Some("node".into()),
                rank: 0,
            }],
        )
        .await
        .into_iter()
        .next()
        .unwrap();

        assert_eq!(service.url.port(), Some(port));
        assert_eq!(service.kind, "Express");
        assert_eq!(service.detail.as_deref(), Some("Checkout"));
        assert_eq!(service.status, 200);
    }
}
