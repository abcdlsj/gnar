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
use tokio::task::JoinHandle;
use tokio_stream::wrappers::ReceiverStream;
use tokio_tungstenite::tungstenite::Message;
use tokio_tungstenite::tungstenite::Utf8Bytes;
use tokio_tungstenite::tungstenite::client::IntoClientRequest;
use tokio_tungstenite::tungstenite::protocol::CloseFrame;
use tokio_tungstenite::tungstenite::protocol::frame::coding::CloseCode;
use tokio_tungstenite::{MaybeTlsStream, WebSocketStream};
use url::Url;

use crate::app::AppError;
use crate::output::{Event, Output};
use crate::protocol::{
    self, ClientFrame, EdgeFrame, ForwardSettings, OpenTunnel, TunnelOpened, WsMessage,
};
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
type WsSender = mpsc::Sender<WsMessage>;

/// Routing state for one tunnel. Each exchange id maps to at most one HTTP
/// body stream or one WebSocket relay, so the two routing tables never collide.
struct ForwardState {
    target: Url,
    client: Client,
    responses: mpsc::Sender<ClientFrame>,
    requests: HashMap<u64, BodySender>,
    websockets: HashMap<u64, WsSender>,
    ws_tasks: HashMap<u64, JoinHandle<()>>,
    settings: ForwardSettings,
}

impl ForwardState {
    fn abort_websockets(&mut self) {
        for (_, task) in self.ws_tasks.drain() {
            task.abort();
        }
        self.websockets.clear();
    }

    /// Drop per-exchange bookkeeping once the edge or the local task reports
    /// the exchange is finished. HTTP forwards leave no bookkeeping behind, so
    /// only WebSocket tasks need this.
    fn observe_client_frame(&mut self, frame: &ClientFrame) {
        let id = match frame {
            ClientFrame::End { id } | ClientFrame::Error { id, .. } => *id,
            _ => return,
        };
        if self.ws_tasks.remove(&id).is_some() {
            self.websockets.remove(&id);
        }
    }
}

pub async fn run(
    target: Url,
    edge: String,
    name: Option<String>,
    settings: ForwardSettings,
    output: &Output,
) -> Result<(), AppError> {
    let websocket_url = websocket_url(&edge)?;
    let name = name.map(normalize_name).transpose()?;
    let token = crate::account::token_for(&edge);
    let (mut writer, mut reader, opened) =
        connect_tunnel(&websocket_url, name, settings, token.as_deref()).await?;
    let mut settings = opened.settings.clone();
    let tunnel_name = opened.name.clone();
    output.event(Event::TunnelReady {
        public_url: &opened.public_url,
        target: target.as_str(),
        account: opened.account.as_deref(),
        reserved: opened.reserved,
    })?;

    let mut ui = output
        .interactive()
        .then(|| {
            LiveUi::new(
                opened.public_url.clone(),
                target.to_string(),
                settings.clone(),
            )
        })
        .transpose()
        .map_err(|error| ui_error("start", error))?;
    if let Some(ui) = &mut ui {
        ui.draw().map_err(|error| ui_error("draw", error))?;
    }

    let (responses, mut outgoing) = mpsc::channel(FRAME_QUEUE);
    let (local_responses, mut local_outgoing) = mpsc::channel(FRAME_QUEUE);
    let mut state = ForwardState {
        target,
        client: Client::new(),
        responses,
        requests: HashMap::new(),
        websockets: HashMap::new(),
        ws_tasks: HashMap::new(),
        settings: settings.clone(),
    };
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
                        spawn_replay(replay, &state.target, &state.client, &local_responses, &state.settings);
                    }
                    Some(Action::ApplySettings(new_settings))
                        if new_settings != settings =>
                    {
                        settings = new_settings.clone();
                        state.settings = settings.clone();
                        state.requests.clear();
                        state.abort_websockets();
                        let Some((restored_writer, restored_reader, opened)) = reconnect(
                            &websocket_url,
                            &tunnel_name,
                            token.as_deref(),
                            &settings,
                            &mut interrupt,
                        )
                        .await?
                        else {
                            return Ok(());
                        };
                        settings = opened.settings.clone();
                        state.settings = settings.clone();
                        (writer, reader) = (restored_writer, restored_reader);
                        ui.set_online(true);
                        ui.set_settings(&settings);
                        ui.notify("settings applied; tunnel reconnected");
                    }
                    Some(Action::ApplySettings(_)) => {}
                    None => {}
                }
                ui.draw().map_err(|error| ui_error("draw", error))?;
            }
            frame = outgoing.recv() => {
                let Some(frame) = frame else { return Ok(()) };
                state.observe_client_frame(&frame);
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
                    handle_edge_frame(&mut state, frame).await;
                    continue;
                }
                if matches!(message, Some(Ok(_))) {
                    continue;
                }

                state.requests.clear();
                state.abort_websockets();
                output.event(Event::EdgeReconnecting)?;
                if let Some(ui) = &mut ui {
                    ui.set_online(false);
                    ui.draw().map_err(|error| ui_error("draw", error))?;
                }
                let Some((restored_writer, restored_reader, opened)) = reconnect(
                    &websocket_url,
                    &tunnel_name,
                    token.as_deref(),
                    &settings,
                    &mut interrupt,
                )
                .await?
                else {
                    return Ok(());
                };
                settings = opened.settings.clone();
                state.settings = settings.clone();
                (writer, reader) = (restored_writer, restored_reader);
                output.event(Event::EdgeRestored)?;
                if let Some(ui) = &mut ui {
                    ui.set_online(true);
                    ui.set_settings(&settings);
                }
            }
        }
    }
}

fn ui_error(action: &str, error: io::Error) -> AppError {
    AppError::Edge(format!("could not {action} the terminal UI: {error}"))
}

struct ForwardRequest {
    id: u64,
    target: Result<Url, String>,
    method: String,
    headers: Vec<protocol::Header>,
    body: reqwest::Body,
}

async fn handle_edge_frame(state: &mut ForwardState, frame: EdgeFrame) {
    match frame {
        EdgeFrame::RequestStart {
            id,
            method,
            path,
            headers,
        } => {
            let (body, incoming) = mpsc::channel(BODY_QUEUE);
            state.requests.insert(id, body);
            tokio::spawn(forward_or_report(
                ForwardRequest {
                    id,
                    target: resolve_target(&state.target, &path),
                    method,
                    headers,
                    body: reqwest::Body::wrap_stream(ReceiverStream::new(incoming)),
                },
                state.client.clone(),
                state.responses.clone(),
                state.settings.clone(),
            ));
        }
        EdgeFrame::RequestChunk { id, body } => {
            if let Some(request) = state.requests.get(&id) {
                let _ = request.send(Ok(Bytes::from(body))).await;
            }
        }
        EdgeFrame::RequestEnd { id } | EdgeFrame::Cancel { id } => {
            state.requests.remove(&id);
            if let Some(task) = state.ws_tasks.remove(&id) {
                task.abort();
                state.websockets.remove(&id);
            }
        }
        EdgeFrame::WsStart {
            id,
            path,
            headers,
            protocol,
        } => {
            if !state.settings.websocket {
                let _ = state
                    .responses
                    .send(ClientFrame::Error {
                        id,
                        reason: "websocket forwarding is disabled for this tunnel".into(),
                    })
                    .await;
                return;
            }
            let (incoming, receiver) = mpsc::channel(FRAME_QUEUE);
            state.websockets.insert(id, incoming);
            let task = tokio::spawn(ws_forward(
                id,
                resolve_target(&state.target, &path),
                headers,
                protocol,
                state.settings.clone(),
                state.responses.clone(),
                receiver,
            ));
            state.ws_tasks.insert(id, task);
        }
        EdgeFrame::WsFrame { id, message } => {
            let Some(sender) = state.websockets.get(&id).cloned() else {
                return;
            };
            if sender.send(message).await.is_err() {
                if let Some(task) = state.ws_tasks.remove(&id) {
                    task.abort();
                }
                state.websockets.remove(&id);
            }
        }
    }
}

async fn forward_or_report(
    request: ForwardRequest,
    client: Client,
    responses: mpsc::Sender<ClientFrame>,
    settings: ForwardSettings,
) {
    let id = request.id;
    let result = forward(request, &client, &responses, &settings).await;
    if let Err(reason) = result {
        let _ = responses.send(ClientFrame::Error { id, reason }).await;
    }
}

async fn forward(
    request: ForwardRequest,
    client: &Client,
    responses: &mpsc::Sender<ClientFrame>,
    settings: &ForwardSettings,
) -> Result<(), String> {
    let ForwardRequest {
        id,
        target,
        method,
        headers,
        body,
    } = request;
    let target = target?;
    let method = Method::from_bytes(method.as_bytes()).map_err(|error| error.to_string())?;
    let mut request = client.request(method, target).body(body);
    for (name, value) in headers {
        if name.eq_ignore_ascii_case("host") {
            if settings.preserve_host {
                request = request.header("host", value);
            }
            continue;
        }
        if name.eq_ignore_ascii_case("content-length") || protocol::hop_by_hop_header(&name) {
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

async fn ws_forward(
    id: u64,
    target: Result<Url, String>,
    headers: Vec<protocol::Header>,
    protocol: Option<String>,
    settings: ForwardSettings,
    responses: mpsc::Sender<ClientFrame>,
    incoming: mpsc::Receiver<WsMessage>,
) {
    let result = ws_forward_inner(
        id, target, headers, protocol, &settings, &responses, incoming,
    )
    .await;
    let frame = match result {
        Ok(()) => ClientFrame::End { id },
        Err(reason) => ClientFrame::Error { id, reason },
    };
    let _ = responses.send(frame).await;
}

async fn ws_forward_inner(
    id: u64,
    target: Result<Url, String>,
    headers: Vec<protocol::Header>,
    protocol: Option<String>,
    settings: &ForwardSettings,
    responses: &mpsc::Sender<ClientFrame>,
    mut incoming: mpsc::Receiver<WsMessage>,
) -> Result<(), String> {
    let mut target = target?;
    let scheme = if target.scheme() == "https" {
        "wss"
    } else {
        "ws"
    };
    target
        .set_scheme(scheme)
        .map_err(|_| "could not convert the target to a WebSocket URL".to_string())?;

    let mut request = target
        .as_str()
        .into_client_request()
        .map_err(|error| format!("could not build the local WebSocket request: {error}"))?;
    for (name, value) in headers {
        let lower = name.to_ascii_lowercase();
        if lower == "host" {
            if settings.preserve_host {
                request.headers_mut().insert("host", header_value(&value)?);
            }
            continue;
        }
        if lower == "sec-websocket-protocol" || protocol::hop_by_hop_header(&name) {
            continue;
        }
        let name: tokio_tungstenite::tungstenite::http::HeaderName = name
            .parse()
            .map_err(|error| format!("invalid forwarded header name: {error}"))?;
        request.headers_mut().insert(name, header_value(&value)?);
    }
    if let Some(selected) = protocol {
        request
            .headers_mut()
            .insert("sec-websocket-protocol", header_value(selected.as_bytes())?);
    }

    let (socket, response) = tokio_tungstenite::connect_async(request)
        .await
        .map_err(|error| format!("local WebSocket handshake failed: {error}"))?;
    let status = response.status().as_u16();
    if status != 101 {
        return Err(format!(
            "local service returned HTTP {status} instead of a WebSocket upgrade"
        ));
    }
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

    let (mut writer, mut reader) = socket.split();
    loop {
        tokio::select! {
            message = incoming.recv() => {
                let Some(message) = message else { break };
                let relayed = tungstenite_message(&message);
                if writer.send(relayed).await.is_err() {
                    break;
                }
                if message.is_close() {
                    break;
                }
            }
            message = reader.next() => {
                let Some(Ok(message)) = message else { break };
                let relayed = ws_message(&message);
                if responses
                    .send(ClientFrame::WsFrame {
                        id,
                        message: relayed.clone(),
                    })
                    .await
                    .is_err()
                {
                    break;
                }
                if relayed.is_close() {
                    break;
                }
            }
        }
    }
    Ok(())
}

fn header_value(value: &[u8]) -> Result<tokio_tungstenite::tungstenite::http::HeaderValue, String> {
    tokio_tungstenite::tungstenite::http::HeaderValue::from_bytes(value)
        .map_err(|error| error.to_string())
}

fn tungstenite_message(message: &WsMessage) -> Message {
    match message {
        WsMessage::Text(text) => Message::text(text.clone()),
        WsMessage::Binary(bytes) => Message::binary(bytes.clone()),
        WsMessage::Ping(bytes) => Message::Ping(bytes.clone().into()),
        WsMessage::Pong(bytes) => Message::Pong(bytes.clone().into()),
        WsMessage::Close { code, reason } => match code {
            Some(code) => Message::Close(Some(CloseFrame {
                code: CloseCode::from(*code),
                reason: Utf8Bytes::from(reason.clone()),
            })),
            None => Message::Close(None),
        },
    }
}

fn ws_message(message: &Message) -> WsMessage {
    match message {
        Message::Text(text) => WsMessage::Text(text.to_string()),
        Message::Binary(bytes) => WsMessage::Binary(bytes.to_vec()),
        Message::Ping(bytes) => WsMessage::Ping(bytes.to_vec()),
        Message::Pong(bytes) => WsMessage::Pong(bytes.to_vec()),
        Message::Close(frame) => match frame {
            Some(frame) => WsMessage::Close {
                code: Some(frame.code.into()),
                reason: frame.reason.to_string(),
            },
            None => WsMessage::Close {
                code: None,
                reason: String::new(),
            },
        },
        Message::Frame(_) => WsMessage::Binary(Vec::new()),
    }
}

fn spawn_replay(
    replay: Replay,
    target: &Url,
    client: &Client,
    responses: &mpsc::Sender<ClientFrame>,
    settings: &ForwardSettings,
) {
    tokio::spawn(forward_or_report(
        ForwardRequest {
            id: replay.id,
            target: resolve_target(target, &replay.path),
            method: replay.method,
            headers: replay.headers,
            body: reqwest::Body::from(replay.body),
        },
        client.clone(),
        responses.clone(),
        settings.clone(),
    ));
}

async fn connect_tunnel(
    websocket_url: &Url,
    name: Option<String>,
    settings: ForwardSettings,
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
        settings,
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
    settings: &ForwardSettings,
    interrupt: &mut (impl Future<Output = io::Result<()>> + Unpin),
) -> Result<Option<Connection>, AppError> {
    let mut delay = Duration::from_millis(200);
    loop {
        let attempt = tokio::select! {
            _ = &mut *interrupt => return Ok(None),
            attempt = tokio::time::timeout(
                CONNECT_TIMEOUT,
                connect_tunnel(
                    websocket_url,
                    Some(name.to_string()),
                    settings.clone(),
                    token,
                ),
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
    let base = url.path().trim_end_matches('/');
    let path = if base.is_empty() {
        "/v1/tunnels".to_string()
    } else {
        format!("{base}/v1/tunnels")
    };
    url.set_path(&path);
    url.set_query(None);
    url.set_fragment(None);
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
    use tokio_tungstenite::tungstenite::Message;
    use url::Url;

    use super::{
        WsMessage, normalize_name, resolve_target, tungstenite_message, websocket_url, ws_message,
    };

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

    #[test]
    fn websocket_endpoint_keeps_an_edge_base_path() {
        assert_eq!(
            websocket_url("https://gnar.example.com/self-hosted/")
                .unwrap()
                .as_str(),
            "wss://gnar.example.com/self-hosted/v1/tunnels"
        );
    }

    #[test]
    fn ws_messages_round_trip_through_tungstenite() {
        for message in [
            WsMessage::Text("hello".into()),
            WsMessage::Binary(vec![0, 1, 2]),
            WsMessage::Ping(vec![9]),
            WsMessage::Pong(vec![8]),
            WsMessage::Close {
                code: Some(1000),
                reason: "done".into(),
            },
            WsMessage::Close {
                code: None,
                reason: String::new(),
            },
        ] {
            let tungsten = tungstenite_message(&message);
            assert_eq!(ws_message(&tungsten), message);
        }
    }

    #[test]
    fn tungstenite_text_is_valid_utf8() {
        let message = tungstenite_message(&WsMessage::Text("终端".into()));
        match message {
            Message::Text(text) => assert_eq!(text.as_str(), "终端"),
            _ => panic!("expected a text frame"),
        }
    }
}
