use std::collections::HashMap;
use std::path::PathBuf;
use std::time::{SystemTime, UNIX_EPOCH};

use rand::Rng;
use rusqlite::{Connection, OptionalExtension, params};
use tokio::sync::{mpsc, oneshot};

const ENDPOINT_LIFETIME_SECONDS: i64 = 3600;
const DEVICE_CODE_LIFETIME_SECONDS: i64 = 600;
const SESSION_RETENTION_SECONDS: i64 = 7 * 24 * 60 * 60;

#[derive(Debug, Default)]
pub struct CleanupStats {
    pub device_authorizations: usize,
    pub endpoints: usize,
    pub sessions: usize,
}

pub struct RequestRecord {
    pub session_id: i64,
    pub method: String,
    pub status: u16,
    pub duration_ms: u64,
    pub bytes_in: u64,
    pub bytes_out: u64,
}

pub struct Account {
    pub id: i64,
    pub name: String,
}

pub struct InviteKeyRecord {
    pub name: String,
    pub secret_hash: String,
    pub max_uses: u32,
    pub expires_at: Option<i64>,
    pub account: String,
}

#[derive(Debug)]
pub enum InviteKeyError {
    Unknown,
    Expired,
    Exhausted,
    Store(String),
}

pub struct DeviceCode {
    pub device_code_hash: String,
    pub user_code: String,
}

pub enum DeviceState {
    Pending,
    Approved { token: String, account: String },
    Denied,
    Expired,
}

#[derive(Debug, PartialEq)]
pub enum NameClaim {
    Granted { endpoint_id: i64, reserved: bool },
    Taken { owner: String },
}

enum Command {
    ClaimEndpoint {
        name: String,
        account_id: Option<i64>,
        reply: Reply<NameClaim>,
    },
    OpenSession(i64, Reply<i64>),
    CloseSession(i64, String, Reply<()>),
    RecordRequest(RequestRecord, Reply<()>),
    AccountForToken(String, Reply<Option<Account>>),
    StartDeviceAuthorization(DeviceCode, Reply<()>),
    ApproveDeviceCode {
        user_code: String,
        account_name: String,
        token_hash: String,
        token: String,
        reply: Reply<Option<String>>,
    },
    EnrollAccount {
        account_name: String,
        token_hash: String,
        reply: Reply<String>,
    },
    SyncInviteKeys(Vec<InviteKeyRecord>, Reply<()>),
    ConsumeInviteKey {
        key: String,
        token: String,
        account: Option<String>,
        reply: InviteKeyReply,
    },
    DenyDeviceCode(String, Reply<bool>),
    PollDeviceCode(String, Reply<DeviceState>),
    #[cfg(test)]
    CountLiveTunnels(i64, Reply<usize>),
    TakeRequestBudget {
        endpoint_id: i64,
        minute: i64,
        limit: u32,
        reply: Reply<bool>,
    },
    ReleaseEndpoint {
        name: String,
        account_id: i64,
        reply: Reply<bool>,
    },
    Cleanup(Reply<CleanupStats>),
}

type Reply<T> = oneshot::Sender<Result<T, String>>;
type InviteKeyReply = oneshot::Sender<Result<String, InviteKeyError>>;

#[derive(Clone)]
pub struct Store {
    commands: mpsc::Sender<Command>,
}

impl Store {
    pub async fn open(path: PathBuf) -> Result<Self, String> {
        let (commands, mut receiver) = mpsc::channel(64);
        let (ready, opened) = oneshot::channel();

        tokio::task::spawn_blocking(move || {
            let connection = match open_connection(path) {
                Ok(connection) => {
                    let _ = ready.send(Ok(()));
                    connection
                }
                Err(error) => {
                    let _ = ready.send(Err(error.to_string()));
                    return;
                }
            };
            let mut issued = HashMap::new();
            while let Some(command) = receiver.blocking_recv() {
                apply(&connection, command, &mut issued);
            }
        });

        opened.await.map_err(|_| worker_stopped())??;
        Ok(Self { commands })
    }

    pub async fn claim_endpoint(
        &self,
        name: String,
        account_id: Option<i64>,
    ) -> Result<NameClaim, String> {
        self.request(|reply| Command::ClaimEndpoint {
            name,
            account_id,
            reply,
        })
        .await
    }

    pub async fn release_endpoint(&self, name: String, account_id: i64) -> Result<bool, String> {
        self.request(|reply| Command::ReleaseEndpoint {
            name,
            account_id,
            reply,
        })
        .await
    }

    pub async fn account_for_token(&self, token: &str) -> Result<Option<Account>, String> {
        let hash = hash_secret(token);
        self.request(|reply| Command::AccountForToken(hash, reply))
            .await
    }

    pub async fn start_device_authorization(&self, code: DeviceCode) -> Result<(), String> {
        self.request(|reply| Command::StartDeviceAuthorization(code, reply))
            .await
    }

    pub async fn approve_device_code(
        &self,
        user_code: String,
        account_name: String,
        token: String,
    ) -> Result<Option<String>, String> {
        let token_hash = hash_secret(&token);
        self.request(|reply| Command::ApproveDeviceCode {
            user_code,
            account_name,
            token_hash,
            token,
            reply,
        })
        .await
    }

    pub async fn enroll_account(
        &self,
        account_name: String,
        token: String,
    ) -> Result<String, String> {
        let token_hash = hash_secret(&token);
        self.request(|reply| Command::EnrollAccount {
            account_name,
            token_hash,
            reply,
        })
        .await
    }

    pub async fn sync_invite_keys(&self, keys: Vec<InviteKeyRecord>) -> Result<(), String> {
        self.request(|reply| Command::SyncInviteKeys(keys, reply))
            .await
    }

    pub async fn consume_invite_key(
        &self,
        key: String,
        token: String,
        account: Option<String>,
    ) -> Result<String, InviteKeyError> {
        let (reply, response) = oneshot::channel();
        self.commands
            .send(Command::ConsumeInviteKey {
                key,
                token,
                account,
                reply,
            })
            .await
            .map_err(|_| InviteKeyError::Store(worker_stopped()))?;
        response
            .await
            .map_err(|_| InviteKeyError::Store(worker_stopped()))?
    }

    pub async fn deny_device_code(&self, user_code: String) -> Result<bool, String> {
        self.request(|reply| Command::DenyDeviceCode(user_code, reply))
            .await
    }

    pub async fn poll_device_code(&self, device_code: &str) -> Result<DeviceState, String> {
        let hash = hash_secret(device_code);
        self.request(|reply| Command::PollDeviceCode(hash, reply))
            .await
    }

    #[cfg(test)]
    async fn count_live_tunnels(&self, account_id: i64) -> Result<usize, String> {
        self.request(|reply| Command::CountLiveTunnels(account_id, reply))
            .await
    }

    pub async fn take_request_budget(
        &self,
        endpoint_id: i64,
        minute: i64,
        limit: u32,
    ) -> Result<bool, String> {
        self.request(|reply| Command::TakeRequestBudget {
            endpoint_id,
            minute,
            limit,
            reply,
        })
        .await
    }

    pub async fn open_session(&self, endpoint_id: i64) -> Result<i64, String> {
        self.request(|reply| Command::OpenSession(endpoint_id, reply))
            .await
    }

    pub async fn close_session(
        &self,
        session_id: i64,
        reason: impl Into<String>,
    ) -> Result<(), String> {
        self.request(|reply| Command::CloseSession(session_id, reason.into(), reply))
            .await
    }

    pub async fn record_request(&self, record: RequestRecord) -> Result<(), String> {
        self.request(|reply| Command::RecordRequest(record, reply))
            .await
    }

    pub async fn cleanup(&self) -> Result<CleanupStats, String> {
        self.request(Command::Cleanup).await
    }

    async fn request<T>(&self, command: impl FnOnce(Reply<T>) -> Command) -> Result<T, String> {
        let (reply, response) = oneshot::channel();
        self.commands
            .send(command(reply))
            .await
            .map_err(|_| worker_stopped())?;
        response.await.map_err(|_| worker_stopped())?
    }
}

fn worker_stopped() -> String {
    "the edge database worker stopped; restart the edge".to_string()
}

pub fn hash_secret(secret: &str) -> String {
    use sha2::{Digest, Sha256};

    let digest = Sha256::digest(secret.as_bytes());
    digest.iter().map(|byte| format!("{byte:02x}")).collect()
}

fn apply(connection: &Connection, command: Command, issued: &mut HashMap<String, String>) {
    fn send<T>(reply: Reply<T>, result: rusqlite::Result<T>) {
        let _ = reply.send(result.map_err(|error| error.to_string()));
    }

    match command {
        Command::ClaimEndpoint {
            name,
            account_id,
            reply,
        } => send(reply, claim_endpoint(connection, &name, account_id)),
        Command::ReleaseEndpoint {
            name,
            account_id,
            reply,
        } => send(reply, release_endpoint(connection, &name, account_id)),
        Command::AccountForToken(hash, reply) => send(reply, account_for_token(connection, &hash)),
        Command::StartDeviceAuthorization(code, reply) => {
            send(reply, start_device_authorization(connection, &code));
        }
        Command::ApproveDeviceCode {
            user_code,
            account_name,
            token_hash,
            token,
            reply,
        } => {
            let result = approve_device_code(connection, &user_code, &account_name, &token_hash);
            if let Ok(Some(approved)) = &result {
                issued.insert(approved.device_code_hash.clone(), token);
            }
            send(
                reply,
                result.map(|approved| approved.map(|approved| approved.account)),
            );
        }
        Command::EnrollAccount {
            account_name,
            token_hash,
            reply,
        } => send(
            reply,
            create_unique_account_token(connection, &account_name, &token_hash, "enrollment"),
        ),
        Command::SyncInviteKeys(keys, reply) => {
            send(reply, sync_invite_keys(connection, &keys));
        }
        Command::ConsumeInviteKey {
            key,
            token,
            account,
            reply,
        } => {
            let _ = reply.send(consume_invite_key(
                connection,
                &key,
                &token,
                account.as_deref(),
            ));
        }
        Command::DenyDeviceCode(user_code, reply) => {
            send(reply, deny_device_code(connection, &user_code));
        }
        Command::PollDeviceCode(hash, reply) => {
            let state = poll_device_code(connection, &hash).map(|state| match state {
                DeviceState::Approved { account, .. } => match issued.remove(&hash) {
                    Some(token) => DeviceState::Approved { token, account },
                    None => DeviceState::Expired,
                },
                other => other,
            });
            send(reply, state);
        }
        #[cfg(test)]
        Command::CountLiveTunnels(account_id, reply) => {
            send(reply, count_live_tunnels(connection, account_id));
        }
        Command::TakeRequestBudget {
            endpoint_id,
            minute,
            limit,
            reply,
        } => send(
            reply,
            take_request_budget(connection, endpoint_id, minute, limit),
        ),
        Command::OpenSession(endpoint_id, reply) => {
            send(reply, open_session(connection, endpoint_id));
        }
        Command::CloseSession(session_id, reason, reply) => {
            send(reply, close_session(connection, session_id, &reason));
        }
        Command::RecordRequest(record, reply) => {
            send(reply, record_request(connection, &record));
        }
        Command::Cleanup(reply) => {
            let result = cleanup(connection);
            if result.is_ok() {
                issued.retain(|hash, _| authorization_exists(connection, hash));
            }
            send(reply, result);
        }
    }
}

fn release_endpoint(
    connection: &Connection,
    name: &str,
    account_id: i64,
) -> rusqlite::Result<bool> {
    connection
        .execute(
            "UPDATE endpoints
             SET account_id = NULL, kind = 'ephemeral', expires_at = unixepoch() + ?3
             WHERE name = ?1 AND account_id = ?2",
            params![name, account_id, ENDPOINT_LIFETIME_SECONDS],
        )
        .map(|rows| rows > 0)
}

fn start_device_authorization(connection: &Connection, code: &DeviceCode) -> rusqlite::Result<()> {
    connection
        .execute(
            "INSERT INTO device_authorizations(
                 device_code_hash, user_code, status, created_at, expires_at
             ) VALUES (?1, ?2, 'pending', unixepoch(), unixepoch() + ?3)",
            params![
                code.device_code_hash,
                code.user_code,
                DEVICE_CODE_LIFETIME_SECONDS
            ],
        )
        .map(|_| ())
}

fn deny_device_code(connection: &Connection, user_code: &str) -> rusqlite::Result<bool> {
    connection
        .execute(
            "UPDATE device_authorizations SET status = 'denied'
             WHERE user_code = ?1 AND status = 'pending'",
            params![user_code],
        )
        .map(|rows| rows > 0)
}

#[cfg(test)]
fn count_live_tunnels(connection: &Connection, account_id: i64) -> rusqlite::Result<usize> {
    connection.query_row(
        "SELECT count(*) FROM tunnel_sessions s
         JOIN endpoints e ON e.id = s.endpoint_id
         WHERE e.account_id = ?1 AND s.disconnected_at IS NULL",
        params![account_id],
        |row| row.get(0),
    )
}

fn close_session(connection: &Connection, session_id: i64, reason: &str) -> rusqlite::Result<()> {
    connection
        .execute(
            "UPDATE tunnel_sessions
             SET disconnected_at = unixepoch(), close_reason = ?1
             WHERE id = ?2",
            params![reason, session_id],
        )
        .map(|_| ())
}

fn record_request(connection: &Connection, record: &RequestRecord) -> rusqlite::Result<()> {
    connection
        .execute(
            "INSERT INTO request_metrics(
                 session_id, method, status, duration_ms, bytes_in, bytes_out, created_at
             ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, unixepoch())",
            params![
                record.session_id,
                record.method,
                record.status,
                record.duration_ms,
                record.bytes_in,
                record.bytes_out
            ],
        )
        .map(|_| ())
}

fn open_connection(path: PathBuf) -> rusqlite::Result<Connection> {
    let mut connection = Connection::open(path)?;
    connection.execute_batch(
        "PRAGMA journal_mode = WAL;
         PRAGMA foreign_keys = ON;
         PRAGMA busy_timeout = 5000;",
    )?;
    migrate(&mut connection)?;
    connection.execute(
        "UPDATE tunnel_sessions
         SET disconnected_at = unixepoch(), close_reason = 'edge restarted'
         WHERE disconnected_at IS NULL",
        [],
    )?;
    cleanup(&connection)?;
    Ok(connection)
}

fn cleanup(connection: &Connection) -> rusqlite::Result<CleanupStats> {
    let device_authorizations = connection.execute(
        "DELETE FROM device_authorizations WHERE expires_at <= unixepoch()",
        [],
    )?;
    let sessions = connection.execute(
        "DELETE FROM tunnel_sessions
         WHERE disconnected_at < unixepoch() - ?1
           AND NOT EXISTS (
               SELECT 1 FROM request_metrics WHERE session_id = tunnel_sessions.id
           )",
        [SESSION_RETENTION_SECONDS],
    )?;
    connection.execute(
        "DELETE FROM request_quota
         WHERE endpoint_id IN (
             SELECT id FROM endpoints
             WHERE expires_at <= unixepoch()
               AND NOT EXISTS (
                   SELECT 1 FROM tunnel_sessions WHERE endpoint_id = endpoints.id
               )
         )",
        [],
    )?;
    let endpoints = connection.execute(
        "DELETE FROM endpoints
         WHERE expires_at <= unixepoch()
           AND NOT EXISTS (
               SELECT 1 FROM tunnel_sessions WHERE endpoint_id = endpoints.id
           )",
        [],
    )?;
    Ok(CleanupStats {
        device_authorizations,
        endpoints,
        sessions,
    })
}

fn authorization_exists(connection: &Connection, hash: &str) -> bool {
    connection
        .query_row(
            "SELECT EXISTS(
                SELECT 1 FROM device_authorizations
                WHERE device_code_hash = ?1 AND expires_at > unixepoch()
            )",
            [hash],
            |row| row.get(0),
        )
        .unwrap_or(false)
}

fn migrate(connection: &mut Connection) -> rusqlite::Result<()> {
    connection.execute_batch(
        "CREATE TABLE IF NOT EXISTS schema_migrations (
             version INTEGER PRIMARY KEY,
             applied_at INTEGER NOT NULL
         );",
    )?;
    let current = connection.query_row(
        "SELECT coalesce(max(version), 0) FROM schema_migrations",
        [],
        |row| row.get::<_, usize>(0),
    )?;
    const MIGRATIONS: [&str; 4] = [
        "CREATE TABLE IF NOT EXISTS endpoints (
             id INTEGER PRIMARY KEY,
             name TEXT NOT NULL UNIQUE,
             kind TEXT NOT NULL,
             created_at INTEGER NOT NULL,
             expires_at INTEGER
         );
         CREATE TABLE IF NOT EXISTS tunnel_sessions (
             id INTEGER PRIMARY KEY,
             endpoint_id INTEGER NOT NULL REFERENCES endpoints(id),
             connected_at INTEGER NOT NULL,
             disconnected_at INTEGER,
             close_reason TEXT
         );",
        "CREATE TABLE IF NOT EXISTS request_metrics (
             id INTEGER PRIMARY KEY,
             session_id INTEGER NOT NULL REFERENCES tunnel_sessions(id),
             method TEXT NOT NULL,
             status INTEGER NOT NULL,
             duration_ms INTEGER NOT NULL,
             bytes_in INTEGER NOT NULL,
             bytes_out INTEGER NOT NULL,
             created_at INTEGER NOT NULL
         );
         CREATE INDEX IF NOT EXISTS request_metrics_session_created
             ON request_metrics(session_id, created_at);",
        "CREATE TABLE IF NOT EXISTS accounts (
             id INTEGER PRIMARY KEY,
             name TEXT NOT NULL UNIQUE,
             created_at INTEGER NOT NULL
         );
         CREATE TABLE IF NOT EXISTS account_tokens (
             id INTEGER PRIMARY KEY,
             account_id INTEGER NOT NULL REFERENCES accounts(id),
             token_hash TEXT NOT NULL UNIQUE,
             label TEXT,
             created_at INTEGER NOT NULL,
             last_used_at INTEGER
         );
         CREATE TABLE IF NOT EXISTS device_authorizations (
             id INTEGER PRIMARY KEY,
             device_code_hash TEXT NOT NULL UNIQUE,
             user_code TEXT NOT NULL UNIQUE,
             account_id INTEGER REFERENCES accounts(id),
             status TEXT NOT NULL,
             created_at INTEGER NOT NULL,
             expires_at INTEGER NOT NULL
         );
         ALTER TABLE endpoints ADD COLUMN account_id INTEGER REFERENCES accounts(id);
         CREATE TABLE IF NOT EXISTS request_quota (
             endpoint_id INTEGER NOT NULL REFERENCES endpoints(id),
             minute INTEGER NOT NULL,
             requests INTEGER NOT NULL,
             PRIMARY KEY (endpoint_id, minute)
         ) WITHOUT ROWID;",
        "CREATE TABLE IF NOT EXISTS invite_keys (
             id INTEGER PRIMARY KEY,
             name TEXT NOT NULL UNIQUE,
             secret_hash TEXT NOT NULL UNIQUE,
             max_uses INTEGER NOT NULL,
             expires_at INTEGER,
             account TEXT NOT NULL,
             used_count INTEGER NOT NULL DEFAULT 0,
             active INTEGER NOT NULL DEFAULT 0,
             created_at INTEGER NOT NULL,
             updated_at INTEGER NOT NULL
         );",
    ];
    for (index, migration) in MIGRATIONS.iter().enumerate().skip(current) {
        let version = index + 1;
        let transaction = connection.transaction()?;
        transaction.execute_batch(migration)?;
        transaction.execute(
            "INSERT INTO schema_migrations(version, applied_at) VALUES (?1, unixepoch())",
            [version],
        )?;
        transaction.commit()?;
    }
    Ok(())
}

fn claim_endpoint(
    connection: &Connection,
    name: &str,
    account_id: Option<i64>,
) -> rusqlite::Result<NameClaim> {
    let existing = connection
        .query_row(
            "SELECT e.id, e.account_id, coalesce(a.name, '')
             FROM endpoints e LEFT JOIN accounts a ON a.id = e.account_id
             WHERE e.name = ?1",
            [name],
            |row| {
                Ok((
                    row.get::<_, i64>(0)?,
                    row.get::<_, Option<i64>>(1)?,
                    row.get::<_, String>(2)?,
                ))
            },
        )
        .ok();

    if let Some((_, Some(owner_id), owner_name)) = &existing
        && Some(*owner_id) != account_id
    {
        return Ok(NameClaim::Taken {
            owner: owner_name.clone(),
        });
    }

    let reserved = account_id.is_some();
    let (kind, expires) = if reserved {
        ("reserved", None)
    } else {
        ("ephemeral", Some(ENDPOINT_LIFETIME_SECONDS))
    };
    connection.execute(
        "INSERT INTO endpoints(name, kind, account_id, created_at, expires_at)
         VALUES (?1, ?2, ?3, unixepoch(),
                 CASE WHEN ?4 IS NULL THEN NULL ELSE unixepoch() + ?4 END)
         ON CONFLICT(name) DO UPDATE SET
             kind = ?2,
             account_id = ?3,
             expires_at = CASE WHEN ?4 IS NULL THEN NULL ELSE unixepoch() + ?4 END",
        params![name, kind, account_id, expires],
    )?;
    let endpoint_id =
        connection.query_row("SELECT id FROM endpoints WHERE name = ?1", [name], |row| {
            row.get(0)
        })?;
    Ok(NameClaim::Granted {
        endpoint_id,
        reserved,
    })
}

fn account_for_token(connection: &Connection, hash: &str) -> rusqlite::Result<Option<Account>> {
    let mut statement = connection.prepare(
        "SELECT t.token_hash, a.id, a.name FROM accounts a
         JOIN account_tokens t ON t.account_id = a.id",
    )?;
    let rows = statement.query_map([], |row| {
        Ok((
            row.get::<_, String>(0)?,
            Account {
                id: row.get(1)?,
                name: row.get(2)?,
            },
        ))
    })?;

    let mut found = None;
    for row in rows {
        let (candidate, account) = row?;
        if constant_time_eq(&candidate, hash) {
            found = Some(account);
        }
    }

    if found.is_some() {
        connection.execute(
            "UPDATE account_tokens SET last_used_at = unixepoch() WHERE token_hash = ?1",
            [hash],
        )?;
    }
    Ok(found)
}

fn constant_time_eq(left: &str, right: &str) -> bool {
    use subtle::ConstantTimeEq;

    left.len() == right.len() && left.as_bytes().ct_eq(right.as_bytes()).into()
}

fn approve_device_code(
    connection: &Connection,
    user_code: &str,
    account_name: &str,
    token_hash: &str,
) -> rusqlite::Result<Option<ApprovedDevice>> {
    let pending = connection
        .query_row(
            "SELECT device_code_hash FROM device_authorizations
             WHERE user_code = ?1 AND status = 'pending' AND expires_at > unixepoch()",
            [user_code],
            |row| row.get::<_, String>(0),
        )
        .ok();
    let Some(device_code_hash) = pending else {
        return Ok(None);
    };

    let account = unique_account_name(connection, account_name)?;
    let account_id = create_account_token(connection, &account, token_hash, "device")?;
    connection.execute(
        "UPDATE device_authorizations SET status = 'approved', account_id = ?2
         WHERE device_code_hash = ?1",
        params![device_code_hash, account_id],
    )?;
    Ok(Some(ApprovedDevice {
        device_code_hash,
        account,
    }))
}

struct ApprovedDevice {
    device_code_hash: String,
    account: String,
}

fn create_unique_account_token(
    connection: &Connection,
    account_name: &str,
    token_hash: &str,
    label: &str,
) -> rusqlite::Result<String> {
    let account = unique_account_name(connection, account_name)?;
    create_account_token(connection, &account, token_hash, label)?;
    Ok(account)
}

fn unique_account_name(connection: &Connection, requested: &str) -> rusqlite::Result<String> {
    if !account_exists(connection, requested) {
        return Ok(requested.to_string());
    }
    const ALPHABET: &[u8] = b"abcdefghjkmnpqrstuvwxyz23456789";
    let mut rng = rand::rng();
    let base = requested
        .chars()
        .take(crate::protocol::MAX_NAME_LENGTH - 5)
        .collect::<String>();
    let base = base.trim_end_matches('-').to_string();
    let base = if base.is_empty() { "acct".into() } else { base };
    for _ in 0..16 {
        let suffix: String = (0..4)
            .map(|_| ALPHABET[rng.random_range(0..ALPHABET.len())] as char)
            .collect();
        let candidate = format!("{base}-{suffix}");
        if crate::protocol::valid_name(&candidate) && !account_exists(connection, &candidate) {
            return Ok(candidate);
        }
    }
    Err(rusqlite::Error::InvalidParameterName(
        "could not find a free account name".into(),
    ))
}

fn account_exists(connection: &Connection, name: &str) -> bool {
    connection
        .query_row(
            "SELECT EXISTS(SELECT 1 FROM accounts WHERE name = ?1)",
            [name],
            |row| row.get(0),
        )
        .unwrap_or(false)
}

fn create_account_token(
    connection: &Connection,
    account_name: &str,
    token_hash: &str,
    label: &str,
) -> rusqlite::Result<i64> {
    connection.execute(
        "INSERT INTO accounts(name, created_at) VALUES (?1, unixepoch())
         ON CONFLICT(name) DO NOTHING",
        [account_name],
    )?;
    let account_id: i64 = connection.query_row(
        "SELECT id FROM accounts WHERE name = ?1",
        [account_name],
        |row| row.get(0),
    )?;
    connection.execute(
        "INSERT INTO account_tokens(account_id, token_hash, label, created_at)
         VALUES (?1, ?2, ?3, unixepoch())",
        params![account_id, token_hash, label],
    )?;
    Ok(account_id)
}

fn sync_invite_keys(connection: &Connection, keys: &[InviteKeyRecord]) -> rusqlite::Result<()> {
    let transaction = connection.unchecked_transaction()?;
    for key in keys {
        let old: Option<(i64, i64)> = transaction
            .query_row(
                "SELECT id, used_count FROM invite_keys
                 WHERE secret_hash = ?1 AND name != ?2",
                params![key.secret_hash, key.name],
                |row| Ok((row.get(0)?, row.get(1)?)),
            )
            .optional()?;
        if let Some((old_id, used_count)) = old {
            let existing: Option<i64> = transaction
                .query_row(
                    "SELECT id FROM invite_keys WHERE name = ?1 AND id != ?2",
                    params![key.name, old_id],
                    |row| row.get(0),
                )
                .optional()?;
            match existing {
                Some(existing_id) => {
                    transaction.execute(
                        "UPDATE invite_keys
                         SET used_count = used_count + ?2, updated_at = unixepoch()
                         WHERE id = ?1",
                        params![existing_id, used_count],
                    )?;
                    transaction.execute("DELETE FROM invite_keys WHERE id = ?1", [old_id])?;
                }
                None => {
                    transaction.execute(
                        "UPDATE invite_keys
                         SET name = ?1, account = ?2, max_uses = ?3, expires_at = ?4,
                             active = 1, updated_at = unixepoch()
                         WHERE id = ?5",
                        params![key.name, key.account, key.max_uses, key.expires_at, old_id],
                    )?;
                }
            }
        }
        transaction.execute(
            "INSERT INTO invite_keys(
                 name, secret_hash, max_uses, expires_at, account,
                 active, created_at, updated_at
             ) VALUES (?1, ?2, ?3, ?4, ?5, 1, unixepoch(), unixepoch())
             ON CONFLICT(name) DO UPDATE SET
                 secret_hash = excluded.secret_hash,
                 max_uses = excluded.max_uses,
                 expires_at = excluded.expires_at,
                 account = excluded.account,
                 active = 1,
                 updated_at = unixepoch()",
            params![
                key.name,
                key.secret_hash,
                key.max_uses,
                key.expires_at,
                key.account
            ],
        )?;
    }
    let active: Vec<String> = transaction
        .prepare("SELECT name FROM invite_keys WHERE active = 1")?
        .query_map([], |row| row.get(0))?
        .collect::<rusqlite::Result<Vec<_>>>()?;
    for name in active {
        if !keys.iter().any(|key| key.name == name) {
            transaction.execute(
                "UPDATE invite_keys SET active = 0, updated_at = unixepoch() WHERE name = ?1",
                [&name],
            )?;
        }
    }
    transaction.commit()
}

fn consume_invite_key(
    connection: &Connection,
    key: &str,
    token: &str,
    account_override: Option<&str>,
) -> Result<String, InviteKeyError> {
    let secret_hash = hash_secret(key);
    let transaction = connection
        .unchecked_transaction()
        .map_err(|error| InviteKeyError::Store(error.to_string()))?;
    let row = transaction
        .query_row(
            "SELECT account, max_uses, used_count, expires_at, active
             FROM invite_keys WHERE secret_hash = ?1",
            [&secret_hash],
            |row| {
                Ok((
                    row.get::<_, String>(0)?,
                    row.get::<_, u32>(1)?,
                    row.get::<_, u32>(2)?,
                    row.get::<_, Option<i64>>(3)?,
                    row.get::<_, bool>(4)?,
                ))
            },
        )
        .optional()
        .map_err(|error| InviteKeyError::Store(error.to_string()))?;
    let Some((account, max_uses, used_count, expires_at, active)) = row else {
        return Err(InviteKeyError::Unknown);
    };
    if !active {
        return Err(InviteKeyError::Unknown);
    }
    if expires_at.is_some_and(|expires| expires <= now_epoch()) {
        return Err(InviteKeyError::Expired);
    }
    if used_count >= max_uses {
        return Err(InviteKeyError::Exhausted);
    }
    let token_hash = hash_secret(token);
    let account = account_override.unwrap_or(&account).to_string();
    let account = create_unique_account_token(&transaction, &account, &token_hash, "invite")
        .map_err(|error| InviteKeyError::Store(error.to_string()))?;
    transaction
        .execute(
            "UPDATE invite_keys SET used_count = used_count + 1, updated_at = unixepoch()
             WHERE secret_hash = ?1",
            [&secret_hash],
        )
        .map_err(|error| InviteKeyError::Store(error.to_string()))?;
    transaction
        .commit()
        .map_err(|error| InviteKeyError::Store(error.to_string()))?;
    Ok(account)
}

fn now_epoch() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_secs() as i64)
        .unwrap_or(0)
}

fn poll_device_code(connection: &Connection, hash: &str) -> rusqlite::Result<DeviceState> {
    let row = connection
        .query_row(
            "SELECT d.status, d.expires_at <= unixepoch(), coalesce(a.name, '')
             FROM device_authorizations d
             LEFT JOIN accounts a ON a.id = d.account_id
             WHERE d.device_code_hash = ?1",
            [hash],
            |row| {
                Ok((
                    row.get::<_, String>(0)?,
                    row.get::<_, bool>(1)?,
                    row.get::<_, String>(2)?,
                ))
            },
        )
        .ok();
    let Some((status, expired, account)) = row else {
        return Ok(DeviceState::Expired);
    };
    Ok(match status.as_str() {
        "denied" => DeviceState::Denied,
        "approved" => DeviceState::Approved {
            token: String::new(),
            account,
        },
        _ if expired => DeviceState::Expired,
        _ => DeviceState::Pending,
    })
}

fn take_request_budget(
    connection: &Connection,
    endpoint_id: i64,
    minute: i64,
    limit: u32,
) -> rusqlite::Result<bool> {
    let used: u32 = connection
        .query_row(
            "SELECT requests FROM request_quota WHERE endpoint_id = ?1 AND minute = ?2",
            params![endpoint_id, minute],
            |row| row.get(0),
        )
        .unwrap_or(0);
    if used >= limit {
        return Ok(false);
    }
    connection.execute(
        "INSERT INTO request_quota(endpoint_id, minute, requests) VALUES (?1, ?2, 1)
         ON CONFLICT(endpoint_id, minute) DO UPDATE SET requests = requests + 1",
        params![endpoint_id, minute],
    )?;
    connection.execute(
        "DELETE FROM request_quota WHERE endpoint_id = ?1 AND minute < ?2",
        params![endpoint_id, minute - 2],
    )?;
    Ok(true)
}

fn open_session(connection: &Connection, endpoint_id: i64) -> rusqlite::Result<i64> {
    connection.execute(
        "INSERT INTO tunnel_sessions(endpoint_id, connected_at) VALUES (?1, unixepoch())",
        [endpoint_id],
    )?;
    Ok(connection.last_insert_rowid())
}

#[cfg(test)]
mod tests {
    use super::{
        DeviceCode, DeviceState, InviteKeyError, InviteKeyRecord, NameClaim, RequestRecord, Store,
        hash_secret,
    };

    fn database(label: &str) -> std::path::PathBuf {
        std::env::temp_dir().join(format!("gnar-store-{label}-{}.db", std::process::id()))
    }

    async fn store(label: &str) -> (Store, std::path::PathBuf) {
        let path = database(label);
        let _ = std::fs::remove_file(&path);
        let store = Store::open(path.clone()).await.unwrap();
        (store, path)
    }

    fn endpoint_id(claim: NameClaim) -> i64 {
        match claim {
            NameClaim::Granted { endpoint_id, .. } => endpoint_id,
            NameClaim::Taken { owner } => panic!("unexpectedly taken by {owner}"),
        }
    }

    async fn sign_in(store: &Store, account: &str) -> i64 {
        let user_code = format!("CODE-{account}");
        store
            .start_device_authorization(DeviceCode {
                device_code_hash: hash_secret(&format!("device-{account}")),
                user_code: user_code.clone(),
            })
            .await
            .unwrap();
        let token = format!("token-{account}");
        store
            .approve_device_code(user_code, account.into(), token.clone())
            .await
            .unwrap()
            .expect("approval");
        store.account_for_token(&token).await.unwrap().unwrap().id
    }

    #[tokio::test]
    async fn lifecycle_is_recorded_and_endpoints_are_reused() {
        let (store, path) = store("lifecycle").await;

        let endpoint = endpoint_id(
            store
                .claim_endpoint("warm-panda-42".into(), None)
                .await
                .unwrap(),
        );
        let reopened = endpoint_id(
            store
                .claim_endpoint("warm-panda-42".into(), None)
                .await
                .unwrap(),
        );
        assert_eq!(endpoint, reopened);

        let session = store.open_session(endpoint).await.unwrap();
        store
            .record_request(RequestRecord {
                session_id: session,
                method: "GET".into(),
                status: 200,
                duration_ms: 18,
                bytes_in: 0,
                bytes_out: 12,
            })
            .await
            .unwrap();
        store.close_session(session, "disconnected").await.unwrap();

        let connection = rusqlite::Connection::open(&path).unwrap();
        let (status, reason): (u16, String) = connection
            .query_row(
                "SELECT m.status, s.close_reason
                 FROM request_metrics m JOIN tunnel_sessions s ON s.id = m.session_id",
                [],
                |row| Ok((row.get(0)?, row.get(1)?)),
            )
            .unwrap();
        assert_eq!(status, 200);
        assert_eq!(reason, "disconnected");

        drop(connection);
        let _ = std::fs::remove_file(&path);
    }

    #[tokio::test]
    async fn tokens_are_stored_only_as_hashes() {
        let (store, path) = store("hashes").await;
        sign_in(&store, "alice").await;

        let connection = rusqlite::Connection::open(&path).unwrap();
        let stored: String = connection
            .query_row("SELECT token_hash FROM account_tokens", [], |row| {
                row.get(0)
            })
            .unwrap();
        assert_eq!(stored, hash_secret("token-alice"));
        assert_ne!(stored, "token-alice");

        assert!(
            store
                .account_for_token("token-alice")
                .await
                .unwrap()
                .is_some()
        );
        assert!(store.account_for_token("wrong").await.unwrap().is_none());

        drop(connection);
        let _ = std::fs::remove_file(&path);
    }

    #[tokio::test]
    async fn duplicate_account_names_get_a_random_four_character_suffix() {
        let (store, path) = store("suffix").await;
        let alice_id = sign_in(&store, "alice").await;

        let account = store
            .enroll_account("alice".into(), "token-alice-2".into())
            .await
            .unwrap();

        assert_ne!(account, "alice");
        assert!(account.starts_with("alice-"));
        assert_eq!(account.len(), "alice-".len() + 4);
        let account_id = store
            .account_for_token("token-alice-2")
            .await
            .unwrap()
            .unwrap()
            .id;
        assert_ne!(account_id, alice_id);

        let _ = std::fs::remove_file(&path);
    }

    #[tokio::test]
    async fn long_account_names_keep_room_for_the_suffix() {
        let (store, path) = store("long-suffix").await;
        let long_name = format!("{}-b", "a".repeat(46));
        assert_eq!(long_name.len(), 48);
        sign_in(&store, &long_name).await;

        let account = store
            .enroll_account(long_name.clone(), "token-long-2".into())
            .await
            .unwrap();

        assert_ne!(account, long_name);
        assert!(account.len() <= 48);
        assert!(account.starts_with(&long_name[..38]));

        let _ = std::fs::remove_file(&path);
    }

    #[tokio::test]
    async fn invite_keys_are_consumed_with_limits_and_expiry() {
        let (store, path) = store("invite").await;
        store
            .sync_invite_keys(vec![InviteKeyRecord {
                name: "demo".into(),
                secret_hash: hash_secret("shared-secret"),
                max_uses: 2,
                expires_at: None,
                account: "demo".into(),
            }])
            .await
            .unwrap();

        let first = store
            .consume_invite_key("shared-secret".into(), "token-demo-1".into(), None)
            .await
            .unwrap();
        let second = store
            .consume_invite_key("shared-secret".into(), "token-demo-2".into(), None)
            .await
            .unwrap();
        assert_ne!(first, second);
        assert!(first.starts_with("demo"));
        assert!(second.starts_with("demo"));
        assert!(matches!(
            store
                .consume_invite_key("shared-secret".into(), "token-demo-3".into(), None)
                .await,
            Err(InviteKeyError::Exhausted)
        ));
        assert!(
            store
                .account_for_token("token-demo-1")
                .await
                .unwrap()
                .is_some()
        );

        store
            .sync_invite_keys(vec![InviteKeyRecord {
                name: "demo".into(),
                secret_hash: hash_secret("shared-secret"),
                max_uses: 2,
                expires_at: Some(super::now_epoch() - 1),
                account: "demo".into(),
            }])
            .await
            .unwrap();
        assert!(matches!(
            store
                .consume_invite_key("shared-secret".into(), "token-demo-4".into(), None)
                .await,
            Err(InviteKeyError::Expired)
        ));

        store.sync_invite_keys(vec![]).await.unwrap();
        assert!(matches!(
            store
                .consume_invite_key("shared-secret".into(), "token-demo-5".into(), None)
                .await,
            Err(InviteKeyError::Unknown)
        ));

        let _ = std::fs::remove_file(&path);
    }

    #[tokio::test]
    async fn a_secret_can_move_to_a_new_key_name() {
        let (store, path) = store("secret-move").await;
        let secret_hash = hash_secret("moved-secret-123");
        store
            .sync_invite_keys(vec![InviteKeyRecord {
                name: "old-name".into(),
                secret_hash: secret_hash.clone(),
                max_uses: 1,
                expires_at: None,
                account: "old-name".into(),
            }])
            .await
            .unwrap();
        assert!(
            store
                .consume_invite_key("moved-secret-123".into(), "token-before-move".into(), None)
                .await
                .is_ok()
        );

        store
            .sync_invite_keys(vec![InviteKeyRecord {
                name: "new-name".into(),
                secret_hash,
                max_uses: 1,
                expires_at: None,
                account: "new-name".into(),
            }])
            .await
            .unwrap();

        assert!(matches!(
            store
                .consume_invite_key("moved-secret-123".into(), "token-after-move".into(), None)
                .await,
            Err(InviteKeyError::Exhausted)
        ));

        let _ = std::fs::remove_file(&path);
    }

    #[tokio::test]
    async fn an_approved_device_code_yields_its_token_once() {
        let (store, path) = store("device-once").await;
        store
            .start_device_authorization(DeviceCode {
                device_code_hash: hash_secret("device-1"),
                user_code: "WDJB-MJHT".into(),
            })
            .await
            .unwrap();

        assert!(matches!(
            store.poll_device_code("device-1").await.unwrap(),
            DeviceState::Pending
        ));

        store
            .approve_device_code("WDJB-MJHT".into(), "alice".into(), "secret-token".into())
            .await
            .unwrap()
            .expect("approval");

        let state = store.poll_device_code("device-1").await.unwrap();
        match state {
            DeviceState::Approved { token, account } => {
                assert_eq!(token, "secret-token");
                assert_eq!(account, "alice");
            }
            _ => panic!("expected approval"),
        }

        assert!(
            matches!(
                store.poll_device_code("device-1").await.unwrap(),
                DeviceState::Expired
            ),
            "a redeemed code must not hand out its token twice"
        );

        let _ = std::fs::remove_file(&path);
    }

    #[tokio::test]
    async fn cleanup_removes_expired_state_without_request_metrics() {
        let (store, path) = store("cleanup").await;
        store
            .start_device_authorization(DeviceCode {
                device_code_hash: hash_secret("expired-device"),
                user_code: "OLD-CODE".into(),
            })
            .await
            .unwrap();
        let endpoint_id = endpoint_id(
            store
                .claim_endpoint("old-endpoint".into(), None)
                .await
                .unwrap(),
        );
        let session_id = store.open_session(endpoint_id).await.unwrap();
        store.close_session(session_id, "done").await.unwrap();
        let connection = rusqlite::Connection::open(&path).unwrap();
        connection
            .execute("UPDATE device_authorizations SET expires_at = 0", [])
            .unwrap();
        connection
            .execute("UPDATE endpoints SET expires_at = 0", [])
            .unwrap();
        connection
            .execute("UPDATE tunnel_sessions SET disconnected_at = 0", [])
            .unwrap();
        drop(connection);

        let stats = store.cleanup().await.unwrap();

        assert_eq!(stats.device_authorizations, 1);
        assert_eq!(stats.sessions, 1);
        assert_eq!(stats.endpoints, 1);
        let connection = rusqlite::Connection::open(path).unwrap();
        for table in ["device_authorizations", "tunnel_sessions", "endpoints"] {
            let count: usize = connection
                .query_row(&format!("SELECT count(*) FROM {table}"), [], |row| {
                    row.get(0)
                })
                .unwrap();
            assert_eq!(count, 0, "{table}");
        }
    }

    #[tokio::test]
    async fn an_unknown_or_denied_code_never_approves() {
        let (store, path) = store("device-denied").await;
        assert!(matches!(
            store.poll_device_code("never-issued").await.unwrap(),
            DeviceState::Expired
        ));

        store
            .start_device_authorization(DeviceCode {
                device_code_hash: hash_secret("device-2"),
                user_code: "AAAA-BBBB".into(),
            })
            .await
            .unwrap();
        assert!(store.deny_device_code("AAAA-BBBB".into()).await.unwrap());
        assert!(matches!(
            store.poll_device_code("device-2").await.unwrap(),
            DeviceState::Denied
        ));
        assert!(
            store
                .approve_device_code("AAAA-BBBB".into(), "mallory".into(), "t".into())
                .await
                .unwrap()
                .is_none(),
            "a denied code cannot later be approved"
        );

        let _ = std::fs::remove_file(&path);
    }

    #[tokio::test]
    async fn a_reserved_name_belongs_to_its_account() {
        let (store, path) = store("reserved").await;
        let alice = sign_in(&store, "alice").await;
        let bob = sign_in(&store, "bob").await;

        let claim = store
            .claim_endpoint("checkout".into(), Some(alice))
            .await
            .unwrap();
        assert!(matches!(claim, NameClaim::Granted { reserved: true, .. }));

        assert_eq!(
            store
                .claim_endpoint("checkout".into(), Some(bob))
                .await
                .unwrap(),
            NameClaim::Taken {
                owner: "alice".into()
            }
        );
        assert_eq!(
            store.claim_endpoint("checkout".into(), None).await.unwrap(),
            NameClaim::Taken {
                owner: "alice".into()
            },
            "anonymous clients cannot take a reserved name"
        );

        assert!(matches!(
            store
                .claim_endpoint("checkout".into(), Some(alice))
                .await
                .unwrap(),
            NameClaim::Granted { reserved: true, .. }
        ));

        assert!(
            store
                .release_endpoint("checkout".into(), alice)
                .await
                .unwrap()
        );
        assert!(matches!(
            store
                .claim_endpoint("checkout".into(), Some(bob))
                .await
                .unwrap(),
            NameClaim::Granted { reserved: true, .. }
        ));

        let _ = std::fs::remove_file(&path);
    }

    #[tokio::test]
    async fn a_reserved_endpoint_does_not_expire() {
        let (store, path) = store("expiry").await;
        let alice = sign_in(&store, "alice").await;
        store
            .claim_endpoint("stable".into(), Some(alice))
            .await
            .unwrap();
        store
            .claim_endpoint("temporary".into(), None)
            .await
            .unwrap();

        let connection = rusqlite::Connection::open(&path).unwrap();
        let mut statement = connection
            .prepare("SELECT name, kind, expires_at IS NULL FROM endpoints ORDER BY name")
            .unwrap();
        let rows = statement
            .query_map([], |row| {
                Ok((
                    row.get::<_, String>(0)?,
                    row.get::<_, String>(1)?,
                    row.get::<_, bool>(2)?,
                ))
            })
            .unwrap()
            .map(Result::unwrap)
            .collect::<Vec<_>>();

        assert_eq!(
            rows,
            vec![
                ("stable".into(), "reserved".into(), true),
                ("temporary".into(), "ephemeral".into(), false),
            ]
        );

        drop(statement);
        drop(connection);
        let _ = std::fs::remove_file(&path);
    }

    #[tokio::test]
    async fn live_tunnels_are_counted_per_account() {
        let (store, path) = store("tunnels").await;
        let alice = sign_in(&store, "alice").await;
        assert_eq!(store.count_live_tunnels(alice).await.unwrap(), 0);

        let first = endpoint_id(
            store
                .claim_endpoint("one".into(), Some(alice))
                .await
                .unwrap(),
        );
        let second = endpoint_id(
            store
                .claim_endpoint("two".into(), Some(alice))
                .await
                .unwrap(),
        );
        let first_session = store.open_session(first).await.unwrap();
        store.open_session(second).await.unwrap();
        assert_eq!(store.count_live_tunnels(alice).await.unwrap(), 2);

        store
            .close_session(first_session, "disconnected")
            .await
            .unwrap();
        assert_eq!(store.count_live_tunnels(alice).await.unwrap(), 1);

        let _ = std::fs::remove_file(&path);
    }

    #[tokio::test]
    async fn request_budget_is_spent_per_minute_and_survives_restart() {
        let (store, path) = store("budget").await;
        let endpoint = endpoint_id(store.claim_endpoint("busy".into(), None).await.unwrap());

        for allowed in 0..3 {
            assert!(
                store.take_request_budget(endpoint, 100, 3).await.unwrap(),
                "request {allowed} is within the limit"
            );
        }
        assert!(
            !store.take_request_budget(endpoint, 100, 3).await.unwrap(),
            "the fourth request exceeds a limit of 3"
        );

        assert!(
            store.take_request_budget(endpoint, 101, 3).await.unwrap(),
            "the next minute starts a fresh bucket"
        );

        drop(store);
        let store = Store::open(path.clone()).await.unwrap();
        assert!(
            !store.take_request_budget(endpoint, 100, 3).await.unwrap(),
            "a restart must not hand out a fresh allowance for a spent minute"
        );

        let _ = std::fs::remove_file(&path);
    }
}
