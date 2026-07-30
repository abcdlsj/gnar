use std::collections::HashMap;
use std::future::Future;
use std::io;
use std::time::Duration;

use bytes::Bytes;
use futures_util::stream::{SplitSink, SplitStream};
use futures_util::{SinkExt, StreamExt};
use reqwest::{Client, Method};
use tokio::net::TcpStream;
use tokio::sync::mpsc;
use tokio_stream::wrappers::ReceiverStream;
use tokio_tungstenite::tungstenite::Message;
use tokio_tungstenite::{MaybeTlsStream, WebSocketStream};
use url::Url;

use crate::app::AppError;
use crate::output::{Event, Output};
use crate::protocol::{self, ClientFrame, EdgeFrame, OpenTunnel, TunnelOpened};
use crate::ui::{Action, LiveUi, Replay};

const FRAME_QUEUE: usize = 128;
const BODY_QUEUE: usize = 16;
const MAX_CHUNK_BYTES: usize = 64 * 1024;
const REDRAW_INTERVAL: Duration = Duration::from_millis(50);
const CONNECT_TIMEOUT: Duration = Duration::from_secs(5);
const MAX_RECONNECT_DELAY: Duration = Duration::from_secs(5);

type Socket = WebSocketStream<MaybeTlsStream<TcpStream>>;
type SocketWriter = SplitSink<Socket, Message>;
type SocketReader = SplitStream<Socket>;
type Connection = (SocketWriter, SocketReader, TunnelOpened);
type BodySender = mpsc::Sender<Result<Bytes, io::Error>>;

pub async fn run(
    target: Url,
    edge: String,
    name: Option<String>,
    output: &Output,
) -> Result<(), AppError> {
    let websocket_url = websocket_url(&edge)?;
    let name = name.map(normalize_name).transpose()?;
    let token = crate::account::token_for(&edge);
    let (mut writer, mut reader, opened) =
        connect_tunnel(&websocket_url, name, token.as_deref()).await?;
    let tunnel_name = opened.name.clone();
    output.event(Event::TunnelReady {
        public_url: &opened.public_url,
        target: target.as_str(),
        account: opened.account.as_deref(),
        reserved: opened.reserved,
    })?;

    let mut ui = output
        .interactive()
        .then(|| LiveUi::new(opened.public_url.clone(), target.to_string()))
        .transpose()
        .map_err(|error| ui_error("start", error))?;
    if let Some(ui) = &mut ui {
        ui.draw().map_err(|error| ui_error("draw", error))?;
    }

    let (responses, mut outgoing) = mpsc::channel(FRAME_QUEUE);
    let (local_responses, mut local_outgoing) = mpsc::channel(FRAME_QUEUE);
    let mut requests = HashMap::<u64, BodySender>::new();
    let client = Client::new();
    let mut redraw = tokio::time::interval(REDRAW_INTERVAL);
    let interrupt = tokio::signal::ctrl_c();
    tokio::pin!(interrupt);

    loop {
        tokio::select! {
            _ = &mut interrupt => return Ok(()),
            _ = redraw.tick() => {
                let Some(ui) = &mut ui else { continue };
                match ui.update() {
                    Some(Action::Quit) => return Ok(()),
                    Some(Action::Replay(replay)) => {
                        spawn_replay(replay, &target, &client, &local_responses);
                    }
                    None => {}
                }
                ui.draw().map_err(|error| ui_error("draw", error))?;
            }
            frame = outgoing.recv() => {
                let Some(frame) = frame else { return Ok(()) };
                if let Some(ui) = &mut ui {
                    ui.apply_client(&frame);
                }
                let bytes = protocol::encode(&frame).map_err(|error| AppError::Edge(error.to_string()))?;
                writer.send(Message::Binary(bytes.into())).await.map_err(|error| AppError::Edge(error.to_string()))?;
            }
            Some(frame) = local_outgoing.recv() => {
                if let Some(ui) = &mut ui {
                    ui.apply_client(&frame);
                }
            }
            message = reader.next() => {
                if let Some(Ok(Message::Binary(bytes))) = message {
                    let frame = protocol::decode::<EdgeFrame>(&bytes).map_err(|error| AppError::Edge(error.to_string()))?;
                    if let Some(ui) = &mut ui {
                        ui.apply_edge(&frame);
                    }
                    handle_edge_frame(frame, &target, &client, &responses, &mut requests).await;
                    continue;
                }
                if matches!(message, Some(Ok(_))) {
                    continue;
                }

                requests.clear();
                output.event(Event::EdgeReconnecting)?;
                if let Some(ui) = &mut ui {
                    ui.set_online(false);
                    ui.draw().map_err(|error| ui_error("draw", error))?;
                }
                let Some((restored_writer, restored_reader, _)) = reconnect(
                    &websocket_url,
                    &tunnel_name,
                    token.as_deref(),
                    &mut interrupt,
                )
                .await?
                else {
                    return Ok(());
                };
                (writer, reader) = (restored_writer, restored_reader);
                output.event(Event::EdgeRestored)?;
                if let Some(ui) = &mut ui {
                    ui.set_online(true);
                }
            }
        }
    }
}

fn ui_error(action: &str, error: io::Error) -> AppError {
    AppError::Edge(format!("could not {action} the terminal UI: {error}"))
}

async fn forward_or_report(
    id: u64,
    target: Result<Url, String>,
    method: String,
    headers: Vec<protocol::Header>,
    body: reqwest::Body,
    client: Client,
    responses: mpsc::Sender<ClientFrame>,
) {
    let result = forward(id, target, method, headers, body, &client, &responses).await;
    if let Err(reason) = result {
        let _ = responses.send(ClientFrame::Error { id, reason }).await;
    }
}

async fn handle_edge_frame(
    frame: EdgeFrame,
    target: &Url,
    client: &Client,
    responses: &mpsc::Sender<ClientFrame>,
    requests: &mut HashMap<u64, mpsc::Sender<Result<Bytes, io::Error>>>,
) {
    match frame {
        EdgeFrame::RequestStart {
            id,
            method,
            path,
            headers,
        } => {
            let (body, incoming) = mpsc::channel(BODY_QUEUE);
            requests.insert(id, body);
            tokio::spawn(forward_or_report(
                id,
                resolve_target(target, &path),
                method,
                headers,
                reqwest::Body::wrap_stream(ReceiverStream::new(incoming)),
                client.clone(),
                responses.clone(),
            ));
        }
        EdgeFrame::RequestChunk { id, body } => {
            if let Some(request) = requests.get(&id) {
                let _ = request.send(Ok(Bytes::from(body))).await;
            }
        }
        EdgeFrame::RequestEnd { id } | EdgeFrame::Cancel { id } => {
            requests.remove(&id);
        }
    }
}

async fn forward(
    id: u64,
    target: Result<Url, String>,
    method: String,
    headers: Vec<protocol::Header>,
    body: reqwest::Body,
    client: &Client,
    responses: &mpsc::Sender<ClientFrame>,
) -> Result<(), String> {
    let target = target?;
    let method = Method::from_bytes(method.as_bytes()).map_err(|error| error.to_string())?;
    let mut request = client.request(method, target).body(body);
    for (name, value) in headers {
        if name.eq_ignore_ascii_case("host")
            || name.eq_ignore_ascii_case("content-length")
            || protocol::hop_by_hop_header(&name)
        {
            continue;
        }
        request = request.header(name, value);
    }
    let mut response = request.send().await.map_err(|error| error.to_string())?;
    let status = response.status().as_u16();
    let headers = response
        .headers()
        .iter()
        .map(|(name, value)| (name.to_string(), value.as_bytes().to_vec()))
        .collect();
    responses
        .send(ClientFrame::Start {
            id,
            status,
            headers,
        })
        .await
        .map_err(|_| "tunnel closed".to_string())?;
    while let Some(chunk) = response.chunk().await.map_err(|error| error.to_string())? {
        for chunk in chunk.chunks(MAX_CHUNK_BYTES) {
            responses
                .send(ClientFrame::Chunk {
                    id,
                    body: chunk.to_vec(),
                })
                .await
                .map_err(|_| "tunnel closed".to_string())?;
        }
    }
    responses
        .send(ClientFrame::End { id })
        .await
        .map_err(|_| "tunnel closed".to_string())
}

fn spawn_replay(
    replay: Replay,
    target: &Url,
    client: &Client,
    responses: &mpsc::Sender<ClientFrame>,
) {
    tokio::spawn(forward_or_report(
        replay.id,
        resolve_target(target, &replay.path),
        replay.method,
        replay.headers,
        reqwest::Body::from(replay.body),
        client.clone(),
        responses.clone(),
    ));
}

async fn connect_tunnel(
    websocket_url: &Url,
    name: Option<String>,
    token: Option<&str>,
) -> Result<Connection, AppError> {
    let edge_error = |error: &dyn std::fmt::Display| AppError::Edge(error.to_string());
    let (socket, _) = tokio_tungstenite::connect_async(websocket_url.as_str())
        .await
        .map_err(|error| unreachable_edge(websocket_url, &error))?;
    let (mut writer, mut reader) = socket.split();
    let open = protocol::encode(&OpenTunnel {
        version: protocol::VERSION,
        name,
        token: token.map(str::to_string),
    })
    .map_err(|error| edge_error(&error))?;
    writer
        .send(Message::Binary(open.into()))
        .await
        .map_err(|error| edge_error(&error))?;
    let Some(Ok(Message::Binary(reply))) = reader.next().await else {
        return Err(AppError::Edge(
            "edge closed the connection before assigning a public endpoint".into(),
        ));
    };
    let opened = match protocol::decode::<protocol::OpenResult>(&reply)
        .map_err(|error| edge_error(&error))?
    {
        protocol::OpenResult::Opened(opened) => opened,
        protocol::OpenResult::Rejected { reason } => return Err(AppError::Edge(reason)),
    };
    if opened.version != protocol::VERSION {
        return Err(AppError::Edge(format!(
            "edge speaks protocol version {} but this client speaks {}; upgrade gnar",
            opened.version,
            protocol::VERSION
        )));
    }
    Ok((writer, reader, opened))
}

async fn reconnect(
    websocket_url: &Url,
    name: &str,
    token: Option<&str>,
    interrupt: &mut (impl Future<Output = io::Result<()>> + Unpin),
) -> Result<Option<Connection>, AppError> {
    let mut delay = Duration::from_millis(200);
    loop {
        let attempt = tokio::select! {
            _ = &mut *interrupt => return Ok(None),
            attempt = tokio::time::timeout(
                CONNECT_TIMEOUT,
                connect_tunnel(websocket_url, Some(name.to_string()), token),
            ) => attempt,
        };
        if let Ok(Ok(connection)) = attempt {
            return Ok(Some(connection));
        }
        tokio::select! {
            _ = &mut *interrupt => return Ok(None),
            _ = tokio::time::sleep(delay) => {}
        }
        delay = (delay * 2).min(MAX_RECONNECT_DELAY);
    }
}

fn unreachable_edge(
    websocket_url: &Url,
    error: &tokio_tungstenite::tungstenite::Error,
) -> AppError {
    let host = websocket_url.host_str().unwrap_or("the edge");
    let scheme = if websocket_url.scheme() == "wss" {
        "https"
    } else {
        "http"
    };
    let port = websocket_url
        .port()
        .map(|port| format!(":{port}"))
        .unwrap_or_default();
    let edge = format!("{scheme}://{host}{port}");

    let advice = if is_unresolved(error) {
        "check the address for a typo, or that its DNS name resolves from here"
    } else {
        "check that an edge is running there and reachable from this machine"
    };
    AppError::Edge(format!("could not reach {edge}: {advice}"))
}

fn is_unresolved(error: &tokio_tungstenite::tungstenite::Error) -> bool {
    error.to_string().contains("lookup address")
}

fn websocket_url(edge: &str) -> Result<Url, AppError> {
    let mut url = Url::parse(edge).map_err(|error| AppError::Edge(error.to_string()))?;
    match url.scheme() {
        "http" => url.set_scheme("ws").unwrap(),
        "https" => url.set_scheme("wss").unwrap(),
        _ => return Err(AppError::Edge("edge must use http:// or https://".into())),
    }
    url.set_path("/v1/tunnels");
    url.set_query(None);
    Ok(url)
}

fn normalize_name(name: String) -> Result<String, AppError> {
    let mut normalized = String::new();
    for character in name.trim().to_ascii_lowercase().chars() {
        if character.is_ascii_alphanumeric() {
            normalized.push(character);
        } else if !normalized.ends_with('-') {
            normalized.push('-');
        }
    }
    let normalized = normalized.trim_matches('-').to_string();
    if normalized.is_empty() || normalized.len() > protocol::MAX_NAME_LENGTH {
        return Err(AppError::Edge(format!(
            "--name must contain 1 to {} letters, numbers, or hyphens",
            protocol::MAX_NAME_LENGTH
        )));
    }
    Ok(normalized)
}

fn resolve_target(base: &Url, path: &str) -> Result<Url, String> {
    let (request_path, request_query) = path.split_once('?').unwrap_or((path, ""));
    let base_path = base.path().trim_end_matches('/');
    let request_path = request_path.trim_start_matches('/');
    let combined = match (base_path, request_path) {
        ("", "") => "/".to_string(),
        ("", request) => format!("/{request}"),
        (base, "") => base.to_string(),
        (base, request) => format!("{base}/{request}"),
    };
    let mut resolved = base.clone();
    resolved.set_path(&combined);
    let query = match (base.query(), request_query) {
        (None, "") => None,
        (Some(base), "") => Some(base.to_string()),
        (None, request) => Some(request.to_string()),
        (Some(base), request) => Some(format!("{base}&{request}")),
    };
    resolved.set_query(query.as_deref());
    Ok(resolved)
}

#[cfg(test)]
mod tests {
    use url::Url;

    use super::{normalize_name, resolve_target};

    #[test]
    fn target_base_path_and_query_are_preserved() {
        let base = Url::parse("http://localhost:3000/api?token=local").unwrap();

        let resolved = resolve_target(&base, "/users?active=true").unwrap();

        assert_eq!(
            resolved.as_str(),
            "http://localhost:3000/api/users?token=local&active=true"
        );
    }

    #[test]
    fn readable_name_is_normalized() {
        assert_eq!(
            normalize_name("My Local API".into()).unwrap(),
            "my-local-api"
        );
    }
}
