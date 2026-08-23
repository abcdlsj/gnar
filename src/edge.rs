use std::collections::HashMap;
use std::convert::Infallible;
use std::fs;
use std::io;
use std::net::{IpAddr, SocketAddr};
use std::num::NonZeroU32;
use std::path::PathBuf;
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, AtomicU16, AtomicU64, Ordering};
use std::time::{Duration, Instant, SystemTime};

use axum::body::Body;
use axum::extract::rejection::JsonRejection;
use axum::extract::ws::{CloseFrame, Message, Utf8Bytes, WebSocket, WebSocketUpgrade};
use axum::extract::{Form, FromRequestParts, Request, State};
use axum::http::header;
use axum::http::{HeaderName, HeaderValue, Method, StatusCode};
use axum::response::{Html, IntoResponse, Response};
use axum::routing::{get, post};
use axum::{Json, Router};
use bytes::Bytes;
use futures_util::{Sink, SinkExt, StreamExt};
use governor::{
    DefaultDirectRateLimiter, DefaultKeyedRateLimiter, Quota as RateQuota, RateLimiter,
};
use rand::Rng;
use serde::Deserialize;
use tokio::sync::{Mutex, OwnedSemaphorePermit, RwLock, Semaphore, mpsc, oneshot};
use tokio_stream::wrappers::ReceiverStream;
use tower_governor::governor::GovernorConfigBuilder;
use tower_governor::key_extractor::{GlobalKeyExtractor, KeyExtractor, SmartIpKeyExtractor};
use tower_governor::{GovernorError, GovernorLayer};

use crate::app::AppError;
use crate::cli::ServeArgs;
use crate::protocol::{
    self, ClientFrame, EdgeFrame, ForwardSettings, OpenTunnel, TunnelOpened, WsMessage,
};
use crate::store::{
    self, DeviceCode, DeviceState, InviteKeyError, NameClaim, RequestRecord, Store,
};

const FRAME_QUEUE: usize = 16;
const BODY_QUEUE: usize = 16;
const WS_FRAME_QUEUE: usize = 2;
const MAX_CHUNK_BYTES: usize = 64 * 1024;
const MAX_HEADERS: usize = 128;
const MAX_HEADER_BYTES: usize = 64 * 1024;
const CLEANUP_INTERVAL: Duration = Duration::from_secs(60 * 60);
const FRAME_SEND_TIMEOUT: Duration = Duration::from_secs(10);
const WS_WRITE_TIMEOUT: Duration = Duration::from_secs(30);
const WS_BUDGET_WINDOW: Duration = Duration::from_secs(60);
const WS_TRAFFIC_LIMIT_REASON: &str = "websocket traffic limit reached";
const WS_MESSAGE_LIMIT_REASON: &str = "websocket message is too large";
const WS_IDLE_REASON: &str = "websocket idle timeout";

pub struct Edge {
    config: ServeArgs,
}

#[derive(Clone)]
struct EdgeState {
    config: Arc<ServeArgs>,
    public_url: String,
    base_path: String,
    base_domain: Option<String>,
    sessions: Arc<RwLock<HashMap<String, Arc<Session>>>>,
    store: Store,
    next_request: Arc<AtomicU64>,
    anonymous_tunnels: Arc<Semaphore>,
    account_tunnels: Arc<Mutex<HashMap<i64, Arc<Semaphore>>>>,
    public_miss_source: Arc<DefaultKeyedRateLimiter<IpAddr>>,
    public_miss_global: Arc<DefaultDirectRateLimiter>,
    websocket_concurrent: usize,
    websocket_idle_timeout: Duration,
    websocket_bytes_per_minute: u64,
    websocket_frames_per_minute: u64,
}

struct Session {
    outgoing: mpsc::Sender<EdgeFrame>,
    pending: Mutex<HashMap<u64, Arc<Pending>>>,
    ws_pending: Mutex<HashMap<u64, mpsc::Sender<WsMessage>>>,
    next_request: Arc<AtomicU64>,
    concurrency: Arc<Semaphore>,
    websocket_concurrency: Arc<Semaphore>,
    session_id: i64,
    endpoint_id: i64,
    requests_per_minute: u32,
    settings: ForwardSettings,
    websocket_idle_timeout: Duration,
    websocket_bytes_per_minute: u64,
    websocket_frames_per_minute: u64,
    store: Store,
    _tunnel_permit: OwnedSemaphorePermit,
}

struct Pending {
    start: Mutex<Option<oneshot::Sender<Result<ResponseHead, String>>>>,
    body: mpsc::Sender<Result<Bytes, io::Error>>,
    _permit: OwnedSemaphorePermit,
    method: String,
    started: Instant,
    status: AtomicU16,
    bytes_in: AtomicU64,
    bytes_out: AtomicU64,
    recorded: AtomicBool,
}

struct WsBudget {
    window_started: Instant,
    bytes: u64,
    frames: u64,
    max_bytes: u64,
    max_frames: u64,
}

impl WsBudget {
    fn new(max_bytes: u64, max_frames: u64) -> Self {
        Self {
            window_started: Instant::now(),
            bytes: 0,
            frames: 0,
            max_bytes,
            max_frames,
        }
    }

    fn allow(&mut self, message: &WsMessage) -> bool {
        if self.window_started.elapsed() >= WS_BUDGET_WINDOW {
            self.window_started = Instant::now();
            self.bytes = 0;
            self.frames = 0;
        }
        let bytes = message.len() as u64;
        if self.frames >= self.max_frames || bytes > self.max_bytes.saturating_sub(self.bytes) {
            return false;
        }
        self.frames += 1;
        self.bytes += bytes;
        true
    }
}

struct ResponseHead {
    status: u16,
    headers: Vec<protocol::Header>,
}

impl Edge {
    pub fn new(config: ServeArgs) -> Self {
        Self { config }
    }

    pub async fn run(self) -> Result<(), AppError> {
        let mut config = self.config;
        if !config.listen.ip().is_loopback() && !config.allow_public_bind {
            return Err(AppError::Edge(format!(
                "refusing to bind {} because it is reachable from the network; \
                 pass --allow-public-bind to accept that, and set --approval-secret \
                 so the device page cannot create accounts on its own",
                config.listen
            )));
        }
        if config.approval_secret.is_none() && !config.anonymous_only {
            config.approval_secret = ask_login_setup()?;
        }

        let config = Arc::new(config);
        let base_path = public_base_path(&config.public_url)?;
        let store = Store::open(config.database.clone())
            .await
            .map_err(AppError::Edge)?;
        let keys_store = store.clone();
        let keys_path = config.keys_file.clone();
        let keys = crate::keys::InviteKeysFile::load(&keys_path).map_err(|error| {
            AppError::Edge(format!(
                "could not load invite keys from {}: {error}; \
                 fix the file or remove it to start the edge",
                keys_path.display()
            ))
        })?;
        keys_store
            .sync_invite_keys(keys.records())
            .await
            .map_err(|error| {
                AppError::Edge(format!(
                    "could not sync invite keys from {}: {error}",
                    keys_path.display()
                ))
            })?;
        println!(
            "  keys     {} configured in {}",
            keys.keys.len(),
            keys_path.display()
        );
        let state = EdgeState {
            public_url: config.public_url.trim_end_matches('/').to_string(),
            base_path: base_path.clone(),
            base_domain: config.base_domain.clone(),
            sessions: Arc::new(RwLock::new(HashMap::new())),
            store,
            next_request: Arc::new(AtomicU64::new(1)),
            anonymous_tunnels: Arc::new(Semaphore::new(config.anonymous_tunnels)),
            account_tunnels: Arc::new(Mutex::new(HashMap::new())),
            public_miss_source: Arc::new(RateLimiter::keyed(RateQuota::per_minute(
                NonZeroU32::new(30).expect("nonzero public miss source limit"),
            ))),
            public_miss_global: Arc::new(RateLimiter::direct(RateQuota::per_minute(
                NonZeroU32::new(300).expect("nonzero public miss global limit"),
            ))),
            websocket_concurrent: config.websocket_concurrent(),
            websocket_idle_timeout: config.websocket_idle_timeout(),
            websocket_bytes_per_minute: config.websocket_bytes_per_minute(),
            websocket_frames_per_minute: config.websocket_frames_per_minute(),
            config: config.clone(),
        };
        tokio::spawn(reload_invite_keys(keys_path, keys_store));
        let mut source_limit = GovernorConfigBuilder::default();
        source_limit.per_second(10).burst_size(6);
        let source_limit = Arc::new(
            source_limit
                .key_extractor(SmartIpKeyExtractor)
                .finish()
                .expect("valid device source rate limit"),
        );
        let source_limiter = source_limit.limiter().clone();
        let public_miss_limiter = state.public_miss_source.clone();
        tokio::spawn(async move {
            loop {
                tokio::time::sleep(CLEANUP_INTERVAL).await;
                source_limiter.retain_recent();
                public_miss_limiter.retain_recent();
            }
        });
        let mut global_limit = GovernorConfigBuilder::default();
        global_limit.per_second(1).burst_size(60);
        let global_limit = Arc::new(
            global_limit
                .key_extractor(GlobalKeyExtractor)
                .finish()
                .expect("valid global device rate limit"),
        );
        let limited_device_code = post(request_device_code)
            .layer::<_, Infallible>(
                GovernorLayer::new(global_limit.clone()).error_handler(device_rate_limited),
            )
            .layer::<_, Infallible>(
                GovernorLayer::new(source_limit.clone()).error_handler(device_rate_limited),
            );
        let limited_device_enrollment = post(enroll_device)
            .layer::<_, Infallible>(
                GovernorLayer::new(global_limit.clone()).error_handler(enrollment_rate_limited),
            )
            .layer::<_, Infallible>(
                GovernorLayer::new(source_limit.clone()).error_handler(enrollment_rate_limited),
            );
        let limited_device_approval = post(approve_device_page)
            .layer::<_, Infallible>(
                GovernorLayer::new(global_limit).error_handler(device_rate_limited),
            )
            .layer::<_, Infallible>(
                GovernorLayer::new(source_limit).error_handler(device_rate_limited),
            );
        let cleanup_store = state.store.clone();
        let routes = Router::new()
            .route("/healthz", get(|| async { "ok" }))
            .route("/v1/tunnels", get(open_tunnel))
            .route("/v1/device/code", limited_device_code)
            .route("/v1/device/enroll", limited_device_enrollment)
            .route("/v1/device/token", post(redeem_device_code))
            .route("/v1/account", get(describe_account))
            .route("/v1/endpoints/release", post(release_endpoint))
            .route("/device", get(device_page).merge(limited_device_approval))
            .fallback(forward_public);
        let app = if base_path.is_empty() {
            routes
        } else {
            Router::new().nest(&base_path, routes.clone()).merge(routes)
        }
        .with_state(state);
        tokio::spawn(cleanup_loop(cleanup_store));
        let listener = tokio::net::TcpListener::bind(config.listen)
            .await
            .map_err(|error| AppError::Edge(error.to_string()))?;

        println!("gnar edge listening on {}", config.listen);
        println!(
            "  quotas   {} tunnels · {}/min signed in, {} tunnel · {}/min anonymous",
            config.account_tunnels,
            config.account_requests,
            config.anonymous_tunnels,
            config.anonymous_requests
        );
        println!(
            "  websocket {} concurrent · {} MiB/min · {} frames/min · {}s idle timeout",
            config.websocket_concurrent(),
            config.websocket_bytes_per_minute() / (1024 * 1024),
            config.websocket_frames_per_minute(),
            config.websocket_idle_timeout().as_secs()
        );
        match &config.approval_secret {
            Some(_) => println!("  login    accounts require the approval secret at /device"),
            None => println!("  login    disabled, this edge serves anonymous tunnels only"),
        }
        axum::serve(
            listener,
            app.into_make_service_with_connect_info::<SocketAddr>(),
        )
        .with_graceful_shutdown(shutdown_signal())
        .await
        .map_err(|error| AppError::Edge(error.to_string()))
    }
}

fn public_base_path(public_url: &str) -> Result<String, AppError> {
    let url = url::Url::parse(public_url)
        .map_err(|error| AppError::Edge(format!("invalid public URL: {error}")))?;
    let path = url.path().trim_end_matches('/');
    Ok(if path.is_empty() {
        String::new()
    } else {
        path.to_string()
    })
}

fn ask_login_setup() -> Result<Option<String>, AppError> {
    use std::io::IsTerminal;

    if !std::io::stdin().is_terminal() || !std::io::stderr().is_terminal() {
        return Ok(None);
    }
    match crate::ui::choose_login_setup(&random_passphrase())
        .map_err(|error| AppError::Edge(format!("could not read the login choice: {error}")))?
    {
        crate::ui::LoginSetup::Anonymous => Ok(None),
        crate::ui::LoginSetup::Secret(secret) => Ok(Some(secret)),
        crate::ui::LoginSetup::Cancelled => Err(AppError::Edge(
            "cancelled before the edge started; pass --anonymous-only or --approval-secret \
             to skip this question"
                .into(),
        )),
    }
}

async fn shutdown_signal() {
    let _ = tokio::signal::ctrl_c().await;
}

async fn cleanup_loop(store: Store) {
    loop {
        tokio::time::sleep(CLEANUP_INTERVAL).await;
        match store.cleanup().await {
            Ok(stats) => tracing::debug!(
                device_authorizations = stats.device_authorizations,
                endpoints = stats.endpoints,
                sessions = stats.sessions,
                "cleaned expired edge state"
            ),
            Err(error) => tracing::error!(%error, "could not clean expired edge state"),
        }
    }
}

async fn reload_invite_keys(path: PathBuf, store: Store) {
    let mut last_modified: Option<SystemTime> = None;
    let mut last_succeeded = false;
    let mut delay = Duration::from_secs(1);
    loop {
        let modified = fs::metadata(&path)
            .and_then(|metadata| metadata.modified())
            .ok();
        if modified != last_modified {
            delay = Duration::from_secs(1);
        }
        if !last_succeeded || modified != last_modified {
            match crate::keys::InviteKeysFile::load(&path) {
                Ok(keys) => match store.sync_invite_keys(keys.records()).await {
                    Ok(()) => {
                        last_modified = modified;
                        last_succeeded = true;
                        delay = Duration::from_secs(1);
                        tracing::debug!("reloaded invite keys from {}", path.display());
                    }
                    Err(error) => {
                        last_succeeded = false;
                        delay = (delay * 2).min(Duration::from_secs(30));
                        tracing::error!(%error, "could not sync invite keys");
                    }
                },
                Err(error) => {
                    last_succeeded = false;
                    delay = (delay * 2).min(Duration::from_secs(30));
                    tracing::warn!(%error, "ignoring invalid invite key file");
                }
            }
        }
        tokio::time::sleep(delay).await;
    }
}

async fn open_tunnel(
    websocket: WebSocketUpgrade,
    State(state): State<EdgeState>,
) -> impl IntoResponse {
    configure_tunnel_websocket(websocket).on_upgrade(move |socket| serve_tunnel(socket, state))
}

fn configure_websocket(websocket: WebSocketUpgrade) -> WebSocketUpgrade {
    configure_websocket_limits(
        websocket,
        protocol::WS_MAX_FRAME_BYTES,
        protocol::WS_MAX_MESSAGE_BYTES,
    )
}

fn configure_tunnel_websocket(websocket: WebSocketUpgrade) -> WebSocketUpgrade {
    configure_websocket_limits(
        websocket,
        protocol::WS_CONTROL_MAX_FRAME_BYTES,
        protocol::WS_CONTROL_MAX_MESSAGE_BYTES,
    )
}

fn configure_websocket_limits(
    websocket: WebSocketUpgrade,
    max_frame_bytes: usize,
    max_message_bytes: usize,
) -> WebSocketUpgrade {
    websocket
        .read_buffer_size(protocol::WS_READ_BUFFER_BYTES)
        .write_buffer_size(protocol::WS_WRITE_BUFFER_BYTES)
        .max_write_buffer_size(protocol::WS_MAX_WRITE_BUFFER_BYTES)
        .max_frame_size(max_frame_bytes)
        .max_message_size(max_message_bytes)
}

struct Granted {
    name: String,
    endpoint_id: i64,
    session_id: i64,
    tunnel_permit: OwnedSemaphorePermit,
    settings: ForwardSettings,
}

async fn negotiate(socket: &mut WebSocket, state: &EdgeState) -> Option<Granted> {
    let Some(Ok(Message::Binary(message))) = socket.next().await else {
        return None;
    };
    let open = protocol::decode::<OpenTunnel>(&message).ok()?;
    if open.version != protocol::VERSION {
        let _ = send_ws_message(socket, Message::Close(None)).await;
        return None;
    }

    let account = match &open.token {
        Some(token) => match state.store.account_for_token(token).await {
            Ok(Some(account)) => Some(account),
            Ok(None) => {
                tracing::warn!("rejected tunnel with an unknown account token");
                reject(
                    socket,
                    "this edge does not recognize the stored token; run `gnar login` again",
                )
                .await;
                return None;
            }
            Err(error) => {
                tracing::error!(%error, "could not validate an account token");
                return None;
            }
        },
        None if state.config.approval_secret.is_some() => {
            tracing::warn!("rejected unauthenticated tunnel on an account-only edge");
            reject(
                socket,
                "this edge requires an account; run `gnar login --edge <url>` first",
            )
            .await;
            return None;
        }
        None => None,
    };

    let tunnel_permit = if let Some(account) = &account {
        let limiter = state
            .account_tunnels
            .lock()
            .await
            .entry(account.id)
            .or_insert_with(|| Arc::new(Semaphore::new(state.config.account_tunnels)))
            .clone();
        match limiter.try_acquire_owned() {
            Ok(permit) => permit,
            Err(_) => {
                tracing::warn!(account = %account.name, "rejected tunnel because its account limit is exhausted");
                reject(
                    socket,
                    &format!(
                        "account {} already has {} tunnels open; stop one and try again",
                        account.name, state.config.account_tunnels
                    ),
                )
                .await;
                return None;
            }
        }
    } else {
        match state.anonymous_tunnels.clone().try_acquire_owned() {
            Ok(permit) => permit,
            Err(_) => {
                tracing::warn!("rejected anonymous tunnel because its limit is exhausted");
                reject(
                    socket,
                    &format!(
                        "this edge already has {} anonymous tunnels open; stop one and try again",
                        state.config.anonymous_tunnels
                    ),
                )
                .await;
                return None;
            }
        }
    };

    let quota = state.config.quota(account.is_some());
    let mut settings = open.settings.clamped();
    settings.requests_per_minute = settings.requests_per_minute.min(quota.requests_per_minute);
    let name = match open.name {
        Some(name) if valid_name(&name) => name,
        Some(_) => {
            reject(
                socket,
                &format!(
                    "--name must be 1 to {} lowercase letters, numbers, or hyphens",
                    protocol::MAX_NAME_LENGTH
                ),
            )
            .await;
            return None;
        }
        None => random_name(),
    };
    let account_id = account.as_ref().map(|account| account.id);
    let (endpoint_id, reserved) = match state.store.claim_endpoint(name.clone(), account_id).await {
        Ok(NameClaim::Granted {
            endpoint_id,
            reserved,
        }) => (endpoint_id, reserved),
        Ok(NameClaim::Taken { owner }) => {
            reject(
                socket,
                &format!("the name {name} is reserved by {owner}; choose another --name"),
            )
            .await;
            return None;
        }
        Err(error) => {
            tracing::error!(%error, "could not claim an endpoint");
            return None;
        }
    };
    let session_id = match state.store.open_session(endpoint_id).await {
        Ok(session_id) => session_id,
        Err(error) => {
            tracing::error!(%error, "could not persist a tunnel session");
            return None;
        }
    };

    let opened = protocol::encode(&protocol::OpenResult::Opened(TunnelOpened {
        version: protocol::VERSION,
        name: name.clone(),
        public_url: public_url(state, &name),
        account: account.map(|account| account.name),
        reserved,
        settings: settings.clone(),
    }))
    .ok()?;
    if let Err(error) = send_ws_message(socket, Message::Binary(opened.into())).await {
        if let Err(close_error) = state
            .store
            .close_session(session_id, "handshake failed")
            .await
        {
            tracing::error!(%close_error, session_id, "could not close a failed tunnel handshake");
        }
        tracing::warn!(%error, "could not finish a tunnel handshake");
        return None;
    }

    Some(Granted {
        name,
        endpoint_id,
        session_id,
        tunnel_permit,
        settings: settings.clone(),
    })
}

async fn serve_tunnel(mut socket: WebSocket, state: EdgeState) {
    let Some(granted) = negotiate(&mut socket, &state).await else {
        return;
    };
    let Granted {
        name,
        endpoint_id,
        session_id,
        tunnel_permit,
        settings,
    } = granted;

    let (outgoing, mut requests) = mpsc::channel(FRAME_QUEUE);
    let session = Arc::new(Session {
        outgoing,
        pending: Mutex::new(HashMap::new()),
        ws_pending: Mutex::new(HashMap::new()),
        next_request: state.next_request.clone(),
        concurrency: Arc::new(Semaphore::new(settings.max_concurrent_exchanges)),
        websocket_concurrency: Arc::new(Semaphore::new(
            settings
                .max_concurrent_exchanges
                .min(state.websocket_concurrent),
        )),
        session_id,
        endpoint_id,
        requests_per_minute: settings.requests_per_minute,
        settings,
        websocket_idle_timeout: state.websocket_idle_timeout,
        websocket_bytes_per_minute: state.websocket_bytes_per_minute,
        websocket_frames_per_minute: state.websocket_frames_per_minute,
        store: state.store.clone(),
        _tunnel_permit: tunnel_permit,
    });
    state
        .sessions
        .write()
        .await
        .insert(name.clone(), session.clone());

    let (mut writer, mut reader) = socket.split();
    let mut writer_task = tokio::spawn(async move {
        while let Some(frame) = requests.recv().await {
            let bytes = protocol::encode(&frame).map_err(|error| error.to_string())?;
            send_ws_message(&mut writer, Message::Binary(bytes.into())).await?;
        }
        Ok::<(), String>(())
    });
    loop {
        tokio::select! {
            _ = &mut writer_task => break,
            message = reader.next() => {
                let Some(Ok(Message::Binary(bytes))) = message else { break };
                let Ok(frame) = protocol::decode::<ClientFrame>(&bytes) else { break };
                handle_client_frame(&session, frame).await;
            }
        }
    }
    writer_task.abort();

    let abandoned = std::mem::take(&mut *session.pending.lock().await);
    for pending in abandoned.into_values() {
        pending
            .respond(Err("the local service disconnected mid-request".into()))
            .await;
    }
    session.ws_pending.lock().await.clear();

    let mut sessions = state.sessions.write().await;
    if sessions
        .get(&name)
        .is_some_and(|current| Arc::ptr_eq(current, &session))
    {
        sessions.remove(&name);
    }
    drop(sessions);
    if let Err(error) = state.store.close_session(session_id, "disconnected").await {
        tracing::error!(%error, session_id, "could not close a tunnel session");
    }
}

async fn handle_client_frame(session: &Session, frame: ClientFrame) {
    match frame {
        ClientFrame::Start {
            id,
            status,
            headers,
        } => {
            let Some(pending) = session.pending(id).await else {
                return;
            };
            pending.status.store(status, Ordering::Relaxed);
            pending.respond(Ok(ResponseHead { status, headers })).await;
        }
        ClientFrame::Chunk { id, body } => {
            let Some(pending) = session.pending(id).await else {
                return;
            };
            pending
                .bytes_out
                .fetch_add(body.len() as u64, Ordering::Relaxed);
            let _ = pending.body.send(Ok(Bytes::from(body))).await;
        }
        ClientFrame::End { id } => {
            session.ws_pending.lock().await.remove(&id);
            if let Some(pending) = session.take_pending(id).await {
                record_request(session, pending, None).await;
            }
        }
        ClientFrame::Error { id, reason } => {
            if let Some(sender) = session.ws_pending.lock().await.remove(&id) {
                let code = if reason.contains("WebSocket message exceeds") {
                    1009
                } else {
                    1011
                };
                let _ = sender.try_send(WsMessage::Close {
                    code: Some(code),
                    reason: truncate_close_reason(&reason),
                });
            }
            if let Some(pending) = session.take_pending(id).await {
                pending.respond(Err(reason)).await;
                record_request(session, pending, Some(502)).await;
            }
        }
        ClientFrame::WsFrame { id, message } => {
            let sender = session.ws_pending.lock().await.get(&id).cloned();
            if let Some(sender) = sender {
                let delivered = tokio::time::timeout(FRAME_SEND_TIMEOUT, sender.send(message))
                    .await
                    .is_ok_and(|result| result.is_ok());
                if !delivered {
                    session.ws_pending.lock().await.remove(&id);
                    let _ = session.send(EdgeFrame::Cancel { id }).await;
                }
            }
        }
    }
}

async fn record_request(session: &Session, pending: Arc<Pending>, status: Option<u16>) {
    if pending.recorded.swap(true, Ordering::Relaxed) {
        return;
    }
    if let Err(error) = session
        .store
        .record_request(RequestRecord {
            session_id: session.session_id,
            method: pending.method.clone(),
            status: status.unwrap_or_else(|| pending.status.load(Ordering::Relaxed)),
            duration_ms: pending.started.elapsed().as_millis().min(u64::MAX.into()) as u64,
            bytes_in: pending.bytes_in.load(Ordering::Relaxed),
            bytes_out: pending.bytes_out.load(Ordering::Relaxed),
        })
        .await
    {
        tracing::error!(%error, session_id = session.session_id, "could not persist request metrics");
    }
}

fn axum_message(message: &WsMessage) -> Message {
    match message {
        WsMessage::Text(text) => Message::Text(Utf8Bytes::from(text.clone())),
        WsMessage::Binary(bytes) => Message::Binary(bytes.clone().into()),
        WsMessage::Ping(bytes) => Message::Ping(bytes.clone().into()),
        WsMessage::Pong(bytes) => Message::Pong(bytes.clone().into()),
        WsMessage::Close { code, reason } => match code {
            Some(code) => Message::Close(Some(CloseFrame {
                code: *code,
                reason: Utf8Bytes::from(reason.clone()),
            })),
            None => Message::Close(None),
        },
    }
}

async fn send_ws_message<S>(writer: &mut S, message: Message) -> Result<(), String>
where
    S: Sink<Message> + Unpin,
    S::Error: std::fmt::Display,
{
    match tokio::time::timeout(WS_WRITE_TIMEOUT, writer.send(message)).await {
        Ok(Ok(())) => Ok(()),
        Ok(Err(error)) => Err(format!("WebSocket write failed: {error}")),
        Err(_) => Err("WebSocket write timed out".into()),
    }
}

fn close_message(code: u16, reason: &str) -> Message {
    Message::Close(Some(CloseFrame {
        code,
        reason: Utf8Bytes::from(truncate_close_reason(reason)),
    }))
}

fn truncate_close_reason(reason: &str) -> String {
    let mut truncated = String::new();
    for character in reason.chars() {
        if truncated.len() + character.len_utf8() > 120 {
            break;
        }
        truncated.push(character);
    }
    truncated
}

fn ws_message(message: &Message) -> WsMessage {
    match message {
        Message::Text(text) => WsMessage::Text(text.to_string()),
        Message::Binary(bytes) => WsMessage::Binary(bytes.to_vec()),
        Message::Ping(bytes) => WsMessage::Ping(bytes.to_vec()),
        Message::Pong(bytes) => WsMessage::Pong(bytes.to_vec()),
        Message::Close(frame) => match frame {
            Some(frame) => WsMessage::Close {
                code: Some(frame.code),
                reason: frame.reason.to_string(),
            },
            None => WsMessage::Close {
                code: None,
                reason: String::new(),
            },
        },
    }
}

async fn forward_public(State(state): State<EdgeState>, request: Request) -> Response {
    let header_bytes = request
        .headers()
        .iter()
        .map(|(name, value)| name.as_str().len() + value.as_bytes().len())
        .sum::<usize>();
    if request.headers().len() > MAX_HEADERS || header_bytes > MAX_HEADER_BYTES {
        return (
            StatusCode::REQUEST_HEADER_FIELDS_TOO_LARGE,
            "request headers are too large",
        )
            .into_response();
    }
    let Some((name, forwarded_path)) = route(&state, &request) else {
        return public_miss(&state, &request);
    };
    let Some(session) = state.sessions.read().await.get(&name).cloned() else {
        return public_miss(&state, &request);
    };
    // A slashless tunnel root is ambiguous for relative URLs: browsers treat
    // `/t/<name>` as a file and resolve `./asset.js` against `/t/`. Redirect
    // GET/HEAD to the canonical trailing-slash form so SPA assets load.
    if forwarded_path == "/"
        && request.uri().path() == format!("/t/{name}")
        && matches!(request.method(), &Method::GET | &Method::HEAD)
        && !is_websocket_upgrade(&request)
    {
        let location = match request.uri().query() {
            Some(query) => format!("{}/t/{name}/?{query}", state.base_path),
            None => format!("{}/t/{name}/", state.base_path),
        };
        return (
            StatusCode::MOVED_PERMANENTLY,
            [(header::LOCATION, location)],
            "",
        )
            .into_response();
    }
    if is_websocket_upgrade(&request) {
        if !session.settings.websocket {
            return tunnel_page(
                StatusCode::BAD_REQUEST,
                "WebSocket forwarding disabled",
                "This tunnel was started with WebSocket forwarding off.",
            );
        }
        let protocols = requested_protocols(&request);
        let headers = websocket_forward_headers(&request);
        let (mut parts, _) = request.into_parts();
        let ws = match WebSocketUpgrade::from_request_parts(&mut parts, &state).await {
            Ok(ws) => ws,
            Err(rejection) => return rejection.into_response(),
        };
        let ws = configure_websocket(ws);
        let budget_available = match session.take_request_budget().await {
            Ok(available) => available,
            Err(error) => {
                tracing::error!(%error, "could not enforce a tunnel request limit");
                return tunnel_page(
                    StatusCode::SERVICE_UNAVAILABLE,
                    "Edge unavailable",
                    "The edge could not verify this tunnel's request limit. Try again shortly.",
                );
            }
        };
        if !budget_available {
            return tunnel_page(
                StatusCode::TOO_MANY_REQUESTS,
                "Rate limit reached",
                &format!(
                    "This tunnel is limited to {} requests per minute. Wait a moment and retry.",
                    session.requests_per_minute
                ),
            );
        }
        let prepared = match session
            .prepare_public_ws(forwarded_path, headers, &protocols)
            .await
        {
            Ok(prepared) => prepared,
            Err((_, "Too many WebSocket connections", _)) => {
                return ws.on_upgrade(|mut socket| async move {
                    let _ = send_ws_message(
                        &mut socket,
                        close_message(1013, "too many concurrent exchanges"),
                    )
                    .await;
                });
            }
            Err((status, title, detail)) => return tunnel_page(status, title, &detail),
        };
        let ws = match prepared.selected_protocol.as_deref() {
            Some(selected) => ws.protocols([selected.to_string()]),
            None => ws,
        };
        let failed_session = session.clone();
        let failed_id = prepared.id;
        return ws
            .on_failed_upgrade(move |_| {
                tokio::spawn(async move {
                    let _ = failed_session
                        .send(EdgeFrame::Cancel { id: failed_id })
                        .await;
                    failed_session.drop_ws_exchange(failed_id).await;
                });
            })
            .on_upgrade(move |socket| {
                let session = session.clone();
                async move {
                    session.serve_public_ws(socket, prepared).await;
                }
            });
    }
    let budget_available = match session.take_request_budget().await {
        Ok(available) => available,
        Err(error) => {
            tracing::error!(%error, "could not enforce a tunnel request limit");
            return tunnel_page(
                StatusCode::SERVICE_UNAVAILABLE,
                "Edge unavailable",
                "The edge could not verify this tunnel's request limit. Try again shortly.",
            );
        }
    };
    if !budget_available {
        return tunnel_page(
            StatusCode::TOO_MANY_REQUESTS,
            "Rate limit reached",
            &format!(
                "This tunnel is limited to {} requests per minute. Wait a moment and retry.",
                session.requests_per_minute
            ),
        );
    }
    match session.forward(request, forwarded_path).await {
        Ok(response) => response,
        Err(reason) => tunnel_page(
            StatusCode::BAD_GATEWAY,
            "Local service unavailable",
            &reason,
        ),
    }
}

fn is_websocket_upgrade(request: &Request) -> bool {
    request.method() == Method::GET
        && request
            .headers()
            .get(header::CONNECTION)
            .and_then(|value| value.to_str().ok())
            .is_some_and(|value| {
                value
                    .split(',')
                    .any(|part| part.trim().eq_ignore_ascii_case("upgrade"))
            })
        && request
            .headers()
            .get(header::UPGRADE)
            .and_then(|value| value.to_str().ok())
            .is_some_and(|value| value.eq_ignore_ascii_case("websocket"))
}

fn requested_protocols(request: &Request) -> Vec<String> {
    request
        .headers()
        .get("sec-websocket-protocol")
        .and_then(|value| value.to_str().ok())
        .map(|value| {
            value
                .split(',')
                .map(str::trim)
                .filter(|part| !part.is_empty())
                .map(str::to_string)
                .collect()
        })
        .unwrap_or_default()
}

fn websocket_forward_headers(request: &Request) -> Vec<protocol::Header> {
    request
        .headers()
        .iter()
        .filter(|(name, _)| {
            let lower = name.as_str().to_ascii_lowercase();
            !protocol::hop_by_hop_header(name.as_str())
                && lower != "sec-websocket-key"
                && lower != "sec-websocket-version"
                && lower != "sec-websocket-extensions"
                && lower != "sec-websocket-protocol"
        })
        .map(|(name, value)| (name.to_string(), value.as_bytes().to_vec()))
        .collect()
}

fn public_miss(state: &EdgeState, request: &Request) -> Response {
    let source = match SmartIpKeyExtractor.extract(request) {
        Ok(source) => source,
        Err(error) => {
            tracing::warn!(reason = %error, "could not identify the source of a public route miss");
            return tunnel_page(
                StatusCode::TOO_MANY_REQUESTS,
                "Too many requests",
                "Wait a moment and retry.",
            );
        }
    };
    if state.public_miss_source.check_key(&source).is_err()
        || state.public_miss_global.check().is_err()
    {
        tracing::warn!(%source, "limited public route misses");
        return tunnel_page(
            StatusCode::TOO_MANY_REQUESTS,
            "Too many requests",
            "Wait a moment and retry.",
        );
    }
    tunnel_page(
        StatusCode::NOT_FOUND,
        "Tunnel not found",
        "Check the address and ask its owner to start gnar again.",
    )
}

async fn describe_account(State(state): State<EdgeState>, request: Request) -> Response {
    let token = bearer_token(&request);
    match state.store.account_for_token(&token).await {
        Ok(Some(account)) => {
            let quota = state.config.quota(true);
            Json(serde_json::json!({
                "account": account.name,
                "tunnels": quota.tunnels,
                "requests_per_minute": quota.requests_per_minute,
            }))
            .into_response()
        }
        Ok(None) => (StatusCode::UNAUTHORIZED, "unknown token").into_response(),
        Err(_) => (StatusCode::INTERNAL_SERVER_ERROR, "could not read account").into_response(),
    }
}

async fn release_endpoint(State(state): State<EdgeState>, request: Request) -> Response {
    let token = bearer_token(&request);
    let Ok(Some(account)) = state.store.account_for_token(&token).await else {
        return (StatusCode::UNAUTHORIZED, "unknown token").into_response();
    };
    let name = request
        .uri()
        .query()
        .and_then(|query| {
            query
                .split('&')
                .filter_map(|pair| pair.split_once('='))
                .find(|(key, _)| *key == "name")
                .map(|(_, value)| value.to_string())
        })
        .unwrap_or_default();

    match state.store.release_endpoint(name, account.id).await {
        Ok(true) => Json(serde_json::json!({ "released": true })).into_response(),
        Ok(false) => (
            StatusCode::NOT_FOUND,
            "that name is not reserved by this account",
        )
            .into_response(),
        Err(_) => (StatusCode::INTERNAL_SERVER_ERROR, "could not release").into_response(),
    }
}

fn bearer_token(request: &Request) -> String {
    request
        .headers()
        .get("authorization")
        .and_then(|value| value.to_str().ok())
        .and_then(|value| value.strip_prefix("Bearer "))
        .unwrap_or_default()
        .to_string()
}

async fn request_device_code(State(state): State<EdgeState>) -> Response {
    if state.config.approval_secret.is_none() {
        return (
            StatusCode::NOT_FOUND,
            "this edge serves anonymous tunnels only; it cannot create accounts",
        )
            .into_response();
    }
    let device_code = random_secret();
    let user_code = random_user_code();
    let record = DeviceCode {
        device_code_hash: store::hash_secret(&device_code),
        user_code: user_code.clone(),
    };
    if state
        .store
        .start_device_authorization(record)
        .await
        .is_err()
    {
        tracing::error!("could not persist a device authorization");
        return (StatusCode::INTERNAL_SERVER_ERROR, "could not start login").into_response();
    }
    Json(serde_json::json!({
        "device_code": device_code,
        "user_code": user_code,
        "verification_uri": format!("{}/device", state.public_url),
        "interval": 2,
    }))
    .into_response()
}

async fn enroll_device(
    State(state): State<EdgeState>,
    body: Result<Json<serde_json::Value>, JsonRejection>,
) -> Response {
    let Json(body) = match body {
        Ok(body) => body,
        Err(_) => {
            return enrollment_json_error(
                StatusCode::BAD_REQUEST,
                "malformed_request",
                "the enrollment request must be valid JSON",
            );
        }
    };
    if state.config.anonymous_only {
        return enrollment_json_error(
            StatusCode::NOT_FOUND,
            "enrollment_disabled",
            "this edge serves anonymous tunnels only",
        );
    }
    if let Some(invite_key) = body.get("invite_key").and_then(|value| value.as_str()) {
        let account = body.get("account").and_then(|value| value.as_str());
        return enroll_with_invite(state, invite_key, account).await;
    }
    let Some(expected) = &state.config.approval_secret else {
        return enrollment_json_error(
            StatusCode::NOT_FOUND,
            "enrollment_disabled",
            "this edge serves anonymous tunnels only; its operator must configure an approval secret",
        );
    };
    let Some(enrollment_key) = body.get("enrollment_key").and_then(|value| value.as_str()) else {
        return enrollment_json_error(
            StatusCode::FORBIDDEN,
            "invalid_enrollment_key",
            "the enrollment key was not accepted",
        );
    };
    if !secrets_match(expected, enrollment_key) {
        tracing::warn!("rejected enrollment with an invalid key");
        return enrollment_json_error(
            StatusCode::FORBIDDEN,
            "invalid_enrollment_key",
            "the enrollment key was not accepted",
        );
    }

    let Some(account) = body.get("account").and_then(|value| value.as_str()) else {
        return enrollment_json_error(
            StatusCode::BAD_REQUEST,
            "malformed_account",
            "account must be 1 to 16 lowercase letters, numbers, or hyphens",
        );
    };
    let Some(account) = normalize_account_name(account) else {
        return enrollment_json_error(
            StatusCode::BAD_REQUEST,
            "malformed_account",
            "account must be 1 to 16 lowercase letters, numbers, or hyphens",
        );
    };

    let token = format!("gnar_{}", random_secret());
    let account = match state.store.enroll_account(account, token.clone()).await {
        Ok(account) => account,
        Err(error) => {
            tracing::error!(%error, "could not persist an enrolled account");
            return enrollment_json_error(
                StatusCode::SERVICE_UNAVAILABLE,
                "edge_unavailable",
                "the edge could not persist the account; try again",
            );
        }
    };
    Json(serde_json::json!({
        "status": "enrolled",
        "account": account,
        "token": token,
    }))
    .into_response()
}

async fn enroll_with_invite(state: EdgeState, invite_key: &str, account: Option<&str>) -> Response {
    if invite_key.is_empty() {
        return enrollment_json_error(
            StatusCode::FORBIDDEN,
            "invalid_invite_key",
            "the invite key was not recognized",
        );
    }
    let account = match account {
        Some(account) => match normalize_account_name(account) {
            Some(account) => Some(account),
            None => {
                return enrollment_json_error(
                    StatusCode::BAD_REQUEST,
                    "malformed_account",
                    "account must be 1 to 16 lowercase letters, numbers, or hyphens",
                );
            }
        },
        None => None,
    };
    let token = format!("gnar_{}", random_secret());
    match state
        .store
        .consume_invite_key(invite_key.to_string(), token.clone(), account)
        .await
    {
        Ok(account) => Json(serde_json::json!({
            "status": "enrolled",
            "account": account,
            "token": token,
        }))
        .into_response(),
        Err(InviteKeyError::Unknown) => enrollment_json_error(
            StatusCode::FORBIDDEN,
            "invalid_invite_key",
            "the invite key was not recognized",
        ),
        Err(InviteKeyError::Expired) => enrollment_json_error(
            StatusCode::FORBIDDEN,
            "invite_key_expired",
            "the invite key has expired",
        ),
        Err(InviteKeyError::Exhausted) => enrollment_json_error(
            StatusCode::FORBIDDEN,
            "invite_key_exhausted",
            "the invite key has reached its usage limit",
        ),
        Err(InviteKeyError::Store(error)) => {
            tracing::error!(%error, "could not consume an invite key");
            enrollment_json_error(
                StatusCode::SERVICE_UNAVAILABLE,
                "edge_unavailable",
                "the edge could not register the account; try again",
            )
        }
    }
}

async fn redeem_device_code(
    State(state): State<EdgeState>,
    Json(body): Json<serde_json::Value>,
) -> Response {
    let Some(device_code) = body.get("device_code").and_then(|value| value.as_str()) else {
        return (StatusCode::BAD_REQUEST, "device_code is required").into_response();
    };
    match state.store.poll_device_code(device_code).await {
        Ok(DeviceState::Approved { token, account }) => {
            Json(serde_json::json!({ "status": "approved", "token": token, "account": account }))
                .into_response()
        }
        Ok(DeviceState::Pending) => {
            Json(serde_json::json!({ "status": "pending" })).into_response()
        }
        Ok(DeviceState::Denied) => Json(serde_json::json!({ "status": "denied" })).into_response(),
        Ok(DeviceState::Expired) => {
            Json(serde_json::json!({ "status": "expired" })).into_response()
        }
        Err(_) => (StatusCode::INTERNAL_SERVER_ERROR, "could not check login").into_response(),
    }
}

async fn device_page(State(state): State<EdgeState>) -> Response {
    if state.config.approval_secret.is_none() {
        return device_html(
            StatusCode::NOT_FOUND,
            "<h1>Login is disabled</h1><p>This edge serves anonymous tunnels only. \
             Its operator can enable accounts by restarting it with an approval secret.</p>",
        );
    }
    device_html(
        StatusCode::OK,
        "<h1>Approve a device</h1>\
         <p>Enter the code shown in your terminal.</p>\
         <form method=post>\
         <label>Code<input name=user_code required autocomplete=off autocapitalize=characters></label>\
         <label>Account name<input name=account required autocomplete=off></label>\
         <label>Approval secret<input name=secret type=password required autocomplete=off></label>\
         <button name=action value=approve>Approve</button>\
         <button name=action value=deny class=ghost>Deny</button>\
         </form>",
    )
}

async fn approve_device_page(
    State(state): State<EdgeState>,
    Form(form): Form<DeviceApproval>,
) -> Response {
    let Some(expected) = &state.config.approval_secret else {
        return device_html(
            StatusCode::NOT_FOUND,
            "<h1>Login is disabled</h1><p>This edge serves anonymous tunnels only.</p>",
        );
    };
    if !secrets_match(expected, &form.secret.clone().unwrap_or_default()) {
        tracing::warn!("rejected device approval with an invalid secret");
        return device_html(
            StatusCode::FORBIDDEN,
            "<h1>Not approved</h1><p>The approval secret does not match.</p>",
        );
    }

    let user_code = form.user_code.trim().to_ascii_uppercase();
    if form.action.as_deref() == Some("deny") {
        let _ = state.store.deny_device_code(user_code).await;
        return device_html(
            StatusCode::OK,
            "<h1>Denied</h1><p>That device will not be signed in.</p>",
        );
    }

    let Some(account) = normalize_account_name(&form.account) else {
        return device_html(
            StatusCode::BAD_REQUEST,
            "<h1>Not approved</h1><p>Use 1 to 16 lowercase letters, numbers, or hyphens \
             for the account name.</p>",
        );
    };

    let token = format!("gnar_{}", random_secret());
    match state
        .store
        .approve_device_code(user_code, account.clone(), token)
        .await
    {
        Ok(Some(account)) => device_html(
            StatusCode::OK,
            &format!(
                "<h1>Approved</h1><p>Signed in as <b>{}</b>. Return to your terminal.</p>",
                escape_html(&account)
            ),
        ),
        Ok(None) => device_html(
            StatusCode::NOT_FOUND,
            "<h1>Not approved</h1><p>That code is unknown, already used, or expired.</p>",
        ),
        Err(_) => device_html(
            StatusCode::INTERNAL_SERVER_ERROR,
            "<h1>Not approved</h1><p>The edge could not complete the login.</p>",
        ),
    }
}

fn device_rate_limited(error: GovernorError) -> Response {
    tracing::warn!(reason = %error, "limited device authorization request");
    (
        StatusCode::TOO_MANY_REQUESTS,
        "too many login attempts; retry later",
    )
        .into_response()
}

fn enrollment_rate_limited(error: GovernorError) -> Response {
    tracing::warn!(reason = %error, "limited enrollment request");
    enrollment_json_error(
        StatusCode::TOO_MANY_REQUESTS,
        "rate_limited",
        "too many enrollment attempts; retry later",
    )
}

fn enrollment_json_error(status: StatusCode, code: &str, message: &str) -> Response {
    (
        status,
        Json(serde_json::json!({
            "status": "error",
            "code": code,
            "message": message,
        })),
    )
        .into_response()
}

#[derive(Deserialize)]
struct DeviceApproval {
    user_code: String,
    #[serde(default)]
    account: String,
    #[serde(default)]
    secret: Option<String>,
    #[serde(default)]
    action: Option<String>,
}

fn secrets_match(expected: &str, provided: &str) -> bool {
    use subtle::ConstantTimeEq;

    expected.len() == provided.len() && expected.as_bytes().ct_eq(provided.as_bytes()).into()
}

fn device_html(status: StatusCode, body: &str) -> Response {
    let page = format!(
        "<!doctype html><meta charset=utf-8><meta name=viewport content='width=device-width'>\
         <title>Sign in · gnar</title><style>\
         body{{margin:0;background:#0d1117;color:#e6edf3;font:16px ui-monospace,SFMono-Regular,Menlo,monospace;display:grid;min-height:100vh;place-items:center}}\
         main{{width:min(420px,calc(100% - 48px))}}\
         b.mark{{background:#7ee787;color:#0d1117;padding:5px 9px}}\
         h1{{font-size:22px;margin:24px 0 10px}}p{{color:#8b949e;line-height:1.6}}\
         label{{display:block;margin:14px 0 0;color:#8b949e;font-size:13px}}\
         input{{width:100%;margin-top:6px;padding:9px;background:#161b22;color:#e6edf3;border:1px solid #30363d;border-radius:6px;font:inherit}}\
         button{{margin:18px 8px 0 0;padding:9px 16px;background:#7ee787;color:#0d1117;border:0;border-radius:6px;font:inherit;font-weight:700;cursor:pointer}}\
         button.ghost{{background:transparent;color:#8b949e;border:1px solid #30363d;font-weight:400}}\
         </style><main><b class=mark>gnar</b>{body}</main>"
    );
    (status, Html(page)).into_response()
}

fn random_secret() -> String {
    let mut rng = rand::rng();
    (0..32)
        .map(|_| char::from_digit(rng.random_range(0..16), 16).unwrap_or('0'))
        .collect()
}

fn random_passphrase() -> String {
    const ALPHABET: &[u8] = b"abcdefghijkmnpqrstuvwxyz23456789";
    let mut rng = rand::rng();
    let mut groups = Vec::with_capacity(4);
    for _ in 0..4 {
        let group: String = (0..5)
            .map(|_| ALPHABET[rng.random_range(0..ALPHABET.len())] as char)
            .collect();
        groups.push(group);
    }
    groups.join("-")
}

fn random_user_code() -> String {
    const ALPHABET: &[u8] = b"BCDFGHJKLMNPQRSTVWXZ23456789";
    let mut rng = rand::rng();
    let mut code = String::with_capacity(9);
    for index in 0..8 {
        if index == 4 {
            code.push('-');
        }
        code.push(ALPHABET[rng.random_range(0..ALPHABET.len())] as char);
    }
    code
}

fn tunnel_page(status: StatusCode, title: &str, detail: &str) -> Response {
    let title = escape_html(title);
    let detail = escape_html(detail);
    let body = format!(
        "<!doctype html><meta charset=utf-8><meta name=viewport content='width=device-width'><title>{title} · gnar</title><style>body{{margin:0;background:#0d1117;color:#e6edf3;font:16px ui-monospace,SFMono-Regular,Menlo,monospace;display:grid;min-height:100vh;place-items:center}}main{{width:min(560px,calc(100% - 48px))}}b{{background:#7ee787;color:#0d1117;padding:5px 9px}}h1{{font-size:24px;margin:28px 0 12px}}p{{color:#8b949e;line-height:1.6}}</style><main><b>gnar</b><h1>{title}</h1><p>{detail}</p></main>"
    );
    (status, Html(body)).into_response()
}

fn escape_html(value: &str) -> String {
    value
        .replace('&', "&amp;")
        .replace('<', "&lt;")
        .replace('>', "&gt;")
        .replace('"', "&quot;")
        .replace('\'', "&#39;")
}

struct PreparedWs {
    id: u64,
    pending: Arc<Pending>,
    incoming: mpsc::Receiver<WsMessage>,
    selected_protocol: Option<String>,
}

impl Session {
    async fn prepare_public_ws(
        &self,
        path: String,
        headers: Vec<protocol::Header>,
        protocols: &[String],
    ) -> Result<PreparedWs, (StatusCode, &'static str, String)> {
        let Ok(permit) = self.websocket_concurrency.clone().try_acquire_owned() else {
            return Err((
                StatusCode::SERVICE_UNAVAILABLE,
                "Too many WebSocket connections",
                "This tunnel has reached its concurrent WebSocket limit. Try again later.".into(),
            ));
        };
        let id = self.next_request.fetch_add(1, Ordering::Relaxed);
        let (start, response_start) = oneshot::channel();
        let (body, _response_body) = mpsc::channel(BODY_QUEUE);
        let pending = Arc::new(Pending {
            start: Mutex::new(Some(start)),
            body,
            _permit: permit,
            method: "WS".into(),
            started: Instant::now(),
            status: AtomicU16::new(0),
            bytes_in: AtomicU64::new(0),
            bytes_out: AtomicU64::new(0),
            recorded: AtomicBool::new(false),
        });
        let (outgoing, incoming) = mpsc::channel::<WsMessage>(WS_FRAME_QUEUE);
        self.pending.lock().await.insert(id, pending.clone());
        self.ws_pending.lock().await.insert(id, outgoing);
        let protocol = (!protocols.is_empty()).then(|| protocols.join(", "));
        if self
            .send(EdgeFrame::WsStart {
                id,
                path,
                headers,
                protocol,
            })
            .await
            .is_err()
        {
            self.drop_ws_exchange(id).await;
            return Err((
                StatusCode::BAD_GATEWAY,
                "Tunnel unavailable",
                "The tunnel disconnected during the WebSocket handshake.".into(),
            ));
        }

        let head = match tokio::time::timeout(
            Duration::from_millis(self.settings.response_head_timeout_ms),
            response_start,
        )
        .await
        {
            Ok(Ok(Ok(head))) if head.status == 101 => head,
            Ok(Ok(Ok(_))) => {
                let _ = self.send(EdgeFrame::Cancel { id }).await;
                self.drop_ws_exchange(id).await;
                return Err((
                    StatusCode::BAD_GATEWAY,
                    "WebSocket upgrade refused",
                    "The local service refused the WebSocket upgrade.".into(),
                ));
            }
            Ok(Ok(Err(reason))) => {
                self.drop_ws_exchange(id).await;
                return Err((StatusCode::BAD_GATEWAY, "Local service unavailable", reason));
            }
            Ok(Err(_)) => {
                self.drop_ws_exchange(id).await;
                return Err((
                    StatusCode::BAD_GATEWAY,
                    "Local service unavailable",
                    "The local service disconnected during the WebSocket handshake.".into(),
                ));
            }
            Err(_) => {
                let _ = self.send(EdgeFrame::Cancel { id }).await;
                self.drop_ws_exchange(id).await;
                return Err((
                    StatusCode::GATEWAY_TIMEOUT,
                    "Local service timed out",
                    "The local service did not answer the WebSocket handshake in time.".into(),
                ));
            }
        };
        pending.status.store(101, Ordering::Relaxed);
        let selected_protocol = head
            .headers
            .iter()
            .find(|(name, _)| name.eq_ignore_ascii_case("sec-websocket-protocol"))
            .and_then(|(_, value)| std::str::from_utf8(value).ok())
            .map(str::trim)
            .filter(|selected| protocols.iter().any(|offered| offered == selected))
            .map(str::to_string);
        Ok(PreparedWs {
            id,
            pending,
            incoming,
            selected_protocol,
        })
    }

    async fn serve_public_ws(self: Arc<Self>, socket: WebSocket, prepared: PreparedWs) {
        let PreparedWs {
            id,
            pending,
            mut incoming,
            ..
        } = prepared;

        let (mut writer, mut reader) = socket.split();
        let mut budget = WsBudget::new(
            self.websocket_bytes_per_minute,
            self.websocket_frames_per_minute,
        );
        let mut idle = Box::pin(tokio::time::sleep(self.websocket_idle_timeout));
        let heartbeat_interval = (self.websocket_idle_timeout / 2)
            .max(Duration::from_millis(100))
            .min(Duration::from_secs(protocol::WS_HEARTBEAT_INTERVAL_SECS));
        let mut heartbeat = tokio::time::interval(heartbeat_interval);
        heartbeat.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
        heartbeat.tick().await;
        let mut heartbeat_nonce = 0u64;
        let mut expected_pong = Vec::new();
        let mut cancel_local = true;
        loop {
            tokio::select! {
                _ = &mut idle => {
                    let _ = send_ws_message(&mut writer, close_message(1001, WS_IDLE_REASON)).await;
                    cancel_local = self
                        .send(EdgeFrame::WsFrame {
                            id,
                            message: WsMessage::Close {
                                code: Some(1001),
                                reason: WS_IDLE_REASON.into(),
                            },
                        })
                        .await
                        .is_err();
                    break;
                }
                _ = heartbeat.tick() => {
                    heartbeat_nonce = heartbeat_nonce.wrapping_add(1);
                    expected_pong = heartbeat_nonce.to_be_bytes().to_vec();
                    if send_ws_message(&mut writer, Message::Ping(expected_pong.clone().into())).await.is_err() {
                        break;
                    }
                }
                message = incoming.recv() => {
                    let Some(message) = message else {
                        cancel_local = false;
                        let _ = send_ws_message(
                            &mut writer,
                            close_message(1011, "local service disconnected"),
                        )
                        .await;
                        break;
                    };
                    pending.bytes_out.fetch_add(message.len() as u64, Ordering::Relaxed);
                    if message.len() > protocol::WS_MAX_FRAME_BYTES {
                        let _ = send_ws_message(
                            &mut writer,
                            close_message(1009, WS_MESSAGE_LIMIT_REASON),
                        )
                        .await;
                        cancel_local = self
                            .send(EdgeFrame::WsFrame {
                                id,
                                message: WsMessage::Close {
                                    code: Some(1009),
                                    reason: WS_MESSAGE_LIMIT_REASON.into(),
                                },
                            })
                            .await
                            .is_err();
                        break;
                    }
                    if !budget.allow(&message) {
                        let _ = send_ws_message(
                            &mut writer,
                            close_message(1008, WS_TRAFFIC_LIMIT_REASON),
                        )
                        .await;
                        cancel_local = self
                            .send(EdgeFrame::WsFrame {
                                id,
                                message: WsMessage::Close {
                                    code: Some(1008),
                                    reason: WS_TRAFFIC_LIMIT_REASON.into(),
                                },
                            })
                            .await
                            .is_err();
                        break;
                    }
                    if send_ws_message(&mut writer, axum_message(&message)).await.is_err() {
                        break;
                    }
                    if message.is_close() {
                        cancel_local = false;
                        break;
                    }
                }
                message = reader.next() => {
                    let message = match message {
                        Some(Ok(message)) => message,
                        Some(Err(error)) => {
                            let reason = error.to_string();
                            if websocket_capacity_error(&reason) {
                                let _ = send_ws_message(
                                    &mut writer,
                                    close_message(1009, WS_MESSAGE_LIMIT_REASON),
                                )
                                .await;
                                cancel_local = self
                                    .send(EdgeFrame::WsFrame {
                                        id,
                                        message: WsMessage::Close {
                                            code: Some(1009),
                                            reason: WS_MESSAGE_LIMIT_REASON.into(),
                                        },
                                    })
                                    .await
                                    .is_err();
                            }
                            break;
                        }
                        None => break,
                    };
                    if matches!(&message, Message::Pong(payload) if payload.as_ref() == expected_pong.as_slice()) {
                        idle.as_mut().reset(tokio::time::Instant::now() + self.websocket_idle_timeout);
                        continue;
                    }
                    let relayed = ws_message(&message);
                    pending.bytes_in.fetch_add(relayed.len() as u64, Ordering::Relaxed);
                    if relayed.len() > protocol::WS_MAX_FRAME_BYTES {
                        let _ = send_ws_message(
                            &mut writer,
                            close_message(1009, WS_MESSAGE_LIMIT_REASON),
                        )
                        .await;
                        cancel_local = self
                            .send(EdgeFrame::WsFrame {
                                id,
                                message: WsMessage::Close {
                                    code: Some(1009),
                                    reason: WS_MESSAGE_LIMIT_REASON.into(),
                                },
                            })
                            .await
                            .is_err();
                        break;
                    }
                    if !budget.allow(&relayed) {
                        let _ = send_ws_message(
                            &mut writer,
                            close_message(1008, WS_TRAFFIC_LIMIT_REASON),
                        )
                        .await;
                        cancel_local = self
                            .send(EdgeFrame::WsFrame {
                                id,
                                message: WsMessage::Close {
                                    code: Some(1008),
                                    reason: WS_TRAFFIC_LIMIT_REASON.into(),
                                },
                            })
                            .await
                            .is_err();
                        break;
                    }
                    if self
                        .send(EdgeFrame::WsFrame {
                            id,
                            message: relayed.clone(),
                        })
                        .await
                        .is_err()
                    {
                        break;
                    }
                    if relayed.is_close() {
                        cancel_local = false;
                        break;
                    }
                }
            }
        }
        if cancel_local {
            let _ = self.send(EdgeFrame::Cancel { id }).await;
        }
        record_request(&self, pending, None).await;
        self.drop_ws_exchange(id).await;
    }

    async fn drop_ws_exchange(&self, id: u64) {
        self.pending.lock().await.remove(&id);
        self.ws_pending.lock().await.remove(&id);
    }

    async fn forward(&self, request: Request, path: String) -> Result<Response, String> {
        let permit = self
            .concurrency
            .clone()
            .acquire_owned()
            .await
            .map_err(|_| "the tunnel closed while the request was queued".to_string())?;
        let id = self.next_request.fetch_add(1, Ordering::Relaxed);
        let method = request.method().to_string();
        let headers = request
            .headers()
            .iter()
            .filter(|(name, _)| !protocol::hop_by_hop_header(name.as_str()))
            .map(|(name, value)| (name.to_string(), value.as_bytes().to_vec()))
            .collect();
        let (start, response_start) = oneshot::channel();
        let (body, response_body) = mpsc::channel(BODY_QUEUE);
        let pending = Arc::new(Pending {
            start: Mutex::new(Some(start)),
            body,
            _permit: permit,
            method: method.clone(),
            started: Instant::now(),
            status: AtomicU16::new(0),
            bytes_in: AtomicU64::new(0),
            bytes_out: AtomicU64::new(0),
            recorded: AtomicBool::new(false),
        });
        self.pending.lock().await.insert(id, pending.clone());
        self.send(EdgeFrame::RequestStart {
            id,
            method,
            path,
            headers,
        })
        .await?;

        let mut request_body = request.into_body().into_data_stream();
        let mut request_bytes = 0;
        while let Some(chunk) = request_body.next().await {
            let chunk = chunk.map_err(|error| error.to_string())?;
            request_bytes += chunk.len();
            pending
                .bytes_in
                .store(request_bytes as u64, Ordering::Relaxed);
            let limit = usize::try_from(self.settings.max_request_bytes).unwrap_or(usize::MAX);
            if request_bytes > limit {
                return Err(self
                    .cancel(
                        id,
                        &format!(
                            "the request body exceeds the {} MiB edge limit",
                            self.settings.max_request_bytes / (1024 * 1024)
                        ),
                    )
                    .await);
            }
            for chunk in chunk.chunks(MAX_CHUNK_BYTES) {
                self.send(EdgeFrame::RequestChunk {
                    id,
                    body: chunk.to_vec(),
                })
                .await?;
            }
        }
        self.send(EdgeFrame::RequestEnd { id }).await?;

        let head = match tokio::time::timeout(
            Duration::from_millis(self.settings.response_head_timeout_ms),
            response_start,
        )
        .await
        {
            Ok(Ok(Ok(head))) => head,
            Ok(Ok(Err(reason))) => return Err(reason),
            Ok(Err(_)) => return Err("the tunnel closed before the response arrived".into()),
            Err(_) => {
                return Err(self
                    .cancel(
                        id,
                        &format!(
                            "the local service did not respond within {}s",
                            self.settings.response_head_timeout_ms / 1000
                        ),
                    )
                    .await);
            }
        };
        let mut response = Response::builder().status(head.status);
        for (name, value) in head.headers {
            if protocol::hop_by_hop_header(&name) {
                continue;
            }
            if let (Ok(name), Ok(value)) =
                (HeaderName::try_from(name), HeaderValue::from_bytes(&value))
            {
                response = response.header(name, value);
            }
        }
        response
            .body(Body::from_stream(ReceiverStream::new(response_body)))
            .map_err(|error| error.to_string())
    }

    async fn send(&self, frame: EdgeFrame) -> Result<(), String> {
        match self.outgoing.try_send(frame) {
            Ok(()) => Ok(()),
            Err(tokio::sync::mpsc::error::TrySendError::Closed(_)) => {
                Err("tunnel disconnected".into())
            }
            Err(tokio::sync::mpsc::error::TrySendError::Full(frame)) => {
                match tokio::time::timeout(FRAME_SEND_TIMEOUT, self.outgoing.send(frame)).await {
                    Ok(Ok(())) => Ok(()),
                    Ok(Err(_)) => Err("tunnel disconnected".into()),
                    Err(_) => Err("tunnel send queue stayed full".into()),
                }
            }
        }
    }

    async fn pending(&self, id: u64) -> Option<Arc<Pending>> {
        self.pending.lock().await.get(&id).cloned()
    }

    async fn take_pending(&self, id: u64) -> Option<Arc<Pending>> {
        self.pending.lock().await.remove(&id)
    }

    async fn cancel(&self, id: u64, reason: &str) -> String {
        self.take_pending(id).await;
        let _ = self.send(EdgeFrame::Cancel { id }).await;
        reason.to_string()
    }

    async fn take_request_budget(&self) -> Result<bool, String> {
        self.store
            .take_request_budget(self.endpoint_id, current_minute(), self.requests_per_minute)
            .await
    }
}

fn websocket_capacity_error(reason: &str) -> bool {
    let reason = reason.to_ascii_lowercase();
    reason.contains("space limit")
        || reason.contains("message too long")
        || reason.contains("capacity")
}

fn current_minute() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|elapsed| elapsed.as_secs() as i64 / 60)
        .unwrap_or_default()
}

async fn reject(socket: &mut WebSocket, reason: &str) {
    let rejected = protocol::OpenResult::Rejected {
        reason: reason.to_string(),
    };
    if let Ok(bytes) = protocol::encode(&rejected) {
        let _ = send_ws_message(socket, Message::Binary(bytes.into())).await;
    }
    let _ = send_ws_message(socket, Message::Close(None)).await;
}

impl Pending {
    async fn respond(&self, head: Result<ResponseHead, String>) {
        if let Some(start) = self.start.lock().await.take() {
            let _ = start.send(head);
        }
    }
}

fn route(state: &EdgeState, request: &Request) -> Option<(String, String)> {
    if let Some(rest) = request.uri().path().strip_prefix("/t/") {
        let (name, suffix) = rest.split_once('/').unwrap_or((rest, ""));
        if !name.is_empty() {
            let path = format!("/{suffix}");
            let query = request
                .uri()
                .query()
                .map(|query| format!("?{query}"))
                .unwrap_or_default();
            return Some((name.to_string(), path + &query));
        }
    }

    let base = state.base_domain.as_deref()?;
    let host = request.headers().get("host")?.to_str().ok()?;
    let host = host.split(':').next()?;
    let name = host.strip_suffix(&format!(".{base}"))?;
    Some((name.to_string(), request.uri().to_string()))
}

fn public_url(state: &EdgeState, name: &str) -> String {
    if let Some(domain) = &state.base_domain {
        let scheme = if state.public_url.starts_with("https://") {
            "https"
        } else {
            "http"
        };
        return format!("{scheme}://{name}.{domain}");
    }
    // A trailing slash keeps relative URLs inside the tunnel's own directory
    // (the browser otherwise treats `/t/<name>` as a file and resolves
    // `./asset.js` against `/t/`).
    format!("{}/t/{name}/", state.public_url)
}

fn random_name() -> String {
    const ADJECTIVES: [&str; 8] = [
        "bright", "calm", "gentle", "lucky", "quiet", "swift", "warm", "wild",
    ];
    const ANIMALS: [&str; 8] = [
        "bear", "fox", "hawk", "koala", "otter", "panda", "raven", "wolf",
    ];
    let mut rng = rand::rng();
    format!(
        "{}-{}-{}",
        ADJECTIVES[rng.random_range(0..ADJECTIVES.len())],
        ANIMALS[rng.random_range(0..ANIMALS.len())],
        rng.random_range(10..100)
    )
}

fn valid_name(name: &str) -> bool {
    protocol::valid_name(name)
}

fn normalize_account_name(name: &str) -> Option<String> {
    let normalized = name.trim().to_ascii_lowercase();
    protocol::valid_account_name(&normalized).then_some(normalized)
}

#[cfg(test)]
mod tests {
    use super::WsBudget;
    use crate::protocol::WsMessage;

    #[test]
    fn websocket_budget_bounds_bytes_and_frames() {
        let mut budget = WsBudget::new(5, 2);
        assert!(budget.allow(&WsMessage::Text("1234".into())));
        assert!(!budget.allow(&WsMessage::Text("12".into())));
        assert!(budget.allow(&WsMessage::Text("1".into())));
        assert!(!budget.allow(&WsMessage::Text("".into())));
    }
}
