use std::collections::{BTreeMap, HashSet};
use std::fs::{self, OpenOptions};
use std::io::{BufRead, Write};
use std::path::Path;
use std::time::{SystemTime, UNIX_EPOCH};

use rand::Rng;
use serde::{Deserialize, Serialize};

use crate::app::AppError;
use crate::cli::{KeyAction, KeyArgs};
use crate::output::{Event, KeySummary, Output};
use crate::protocol::{MAX_NAME_LENGTH, valid_name};
use crate::store::{InviteKeyRecord, hash_secret};

const MAX_SECRET_BYTES: usize = 4096;
const MIN_SECRET_LENGTH: usize = 12;

#[derive(Debug, Default, Deserialize, Serialize)]
pub struct InviteKeysFile {
    #[serde(default)]
    pub keys: BTreeMap<String, InviteKey>,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct InviteKey {
    #[serde(default)]
    pub secret: String,
    #[serde(default = "default_max_uses")]
    pub max_uses: u32,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub expires_at: Option<i64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub account: Option<String>,
}

impl Default for InviteKey {
    fn default() -> Self {
        Self {
            secret: String::new(),
            max_uses: 1,
            expires_at: None,
            account: None,
        }
    }
}

fn default_max_uses() -> u32 {
    1
}

impl InviteKeysFile {
    pub fn load(path: &Path) -> Result<Self, String> {
        match fs::metadata(path) {
            Ok(_) => {
                check_private_permissions(path)?;
                let content = fs::read_to_string(path)
                    .map_err(|error| format!("could not read {}: {error}", path.display()))?;
                let file: Self = serde_json::from_str(&content)
                    .map_err(|error| format!("invalid JSON in {}: {error}", path.display()))?;
                file.validate()?;
                Ok(file)
            }
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(Self::default()),
            Err(error) => Err(format!("could not read {}: {error}", path.display())),
        }
    }

    pub fn validate(&self) -> Result<(), String> {
        let mut secrets = HashSet::new();
        for (name, key) in &self.keys {
            if !valid_name(name) {
                return Err(format!(
                    "invite key name {name:?} must be 1 to {MAX_NAME_LENGTH} \
                     lowercase letters, numbers, or hyphens"
                ));
            }
            let secret = key.secret.trim();
            if secret.is_empty() {
                return Err(format!("invite key {name} needs a non-empty secret"));
            }
            if secret.len() < MIN_SECRET_LENGTH {
                return Err(format!(
                    "invite key {name} secret must be at least {MIN_SECRET_LENGTH} characters"
                ));
            }
            if key.max_uses == 0 {
                return Err(format!("invite key {name} needs max_uses >= 1"));
            }
            if let Some(account) = &key.account
                && !valid_name(account)
            {
                return Err(format!(
                    "invite key {name} account {account:?} must be 1 to {MAX_NAME_LENGTH} \
                     lowercase letters, numbers, or hyphens"
                ));
            }
            if !secrets.insert(secret) {
                return Err(format!("invite key {name} reuses another key's secret"));
            }
        }
        Ok(())
    }

    pub fn records(&self) -> Vec<InviteKeyRecord> {
        self.keys
            .iter()
            .map(|(name, key)| InviteKeyRecord {
                name: name.clone(),
                secret_hash: hash_secret(key.secret.trim()),
                max_uses: key.max_uses,
                expires_at: key.expires_at,
                account: key.account.clone().unwrap_or_else(|| name.clone()),
            })
            .collect()
    }

    pub fn write(&self, path: &Path) -> Result<(), String> {
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent)
                .map_err(|error| format!("could not create {}: {error}", parent.display()))?;
        }
        let content = serde_json::to_string_pretty(self)
            .map_err(|error| format!("could not encode invite keys: {error}"))?;
        let temporary = path.with_extension(format!(
            "json.tmp-{}-{}",
            std::process::id(),
            rand::random::<u64>()
        ));
        let result = write_private(&temporary, content.as_bytes()).and_then(|_| {
            fs::rename(&temporary, path)
                .map_err(|error| format!("could not replace {}: {error}", path.display()))
        });
        if result.is_err() {
            let _ = fs::remove_file(&temporary);
        }
        result
    }
}

pub fn run(args: KeyArgs, output: &Output) -> Result<(), AppError> {
    let path = args.file;
    match args.action {
        KeyAction::Add {
            name,
            max_uses,
            expires_in,
            account,
            secret_stdin,
            show_secret,
        } => {
            let mut file = InviteKeysFile::load(&path).map_err(AppError::Key)?;
            let name = normalize_name(&name).map_err(AppError::Key)?;
            let mut key = file.keys.get(&name).cloned().unwrap_or_default();
            if secret_stdin {
                key.secret = read_secret_line()?;
            }
            if key.secret.is_empty() {
                key.secret = random_secret();
            }
            if let Some(max_uses) = max_uses {
                key.max_uses = max_uses;
            }
            if let Some(expires_in) = expires_in {
                let duration = i64::try_from(parse_duration(&expires_in).map_err(AppError::Key)?)
                    .map_err(|_| {
                    AppError::Key(format!("duration {expires_in:?} is too large"))
                })?;
                key.expires_at = Some(now_epoch().checked_add(duration).ok_or_else(|| {
                    AppError::Key(format!("duration {expires_in:?} is too large"))
                })?);
            }
            if let Some(account) = account {
                key.account = Some(normalize_name(&account).map_err(AppError::Key)?);
            }
            let account_name = key.account.clone().unwrap_or_else(|| name.clone());
            file.keys.insert(name.clone(), key.clone());
            file.validate().map_err(AppError::Key)?;
            file.write(&path).map_err(AppError::Key)?;
            let secret = if show_secret {
                Some(key.secret.as_str())
            } else {
                None
            };
            output.event(Event::KeyAdded {
                name: &name,
                account: &account_name,
                max_uses: key.max_uses,
                expires_at: key.expires_at,
                secret,
            })?;
            Ok(())
        }
        KeyAction::List => {
            let file = InviteKeysFile::load(&path).map_err(AppError::Key)?;
            let keys = file
                .keys
                .iter()
                .map(|(name, key)| KeySummary {
                    name: name.clone(),
                    account: key.account.clone().unwrap_or_else(|| name.clone()),
                    max_uses: key.max_uses,
                    expires_at: key.expires_at,
                })
                .collect();
            output.event(Event::KeyList { keys })?;
            Ok(())
        }
        KeyAction::Revoke { name } => {
            let mut file = InviteKeysFile::load(&path).map_err(AppError::Key)?;
            let name = normalize_name(&name).map_err(AppError::Key)?;
            if file.keys.remove(&name).is_some() {
                file.write(&path).map_err(AppError::Key)?;
                output.event(Event::KeyRevoked { name: &name })?;
                Ok(())
            } else {
                Err(AppError::Key(format!("no invite key named {name}")))
            }
        }
        KeyAction::Show { name } => {
            let file = InviteKeysFile::load(&path).map_err(AppError::Key)?;
            let name = normalize_name(&name).map_err(AppError::Key)?;
            let key = file
                .keys
                .get(&name)
                .ok_or_else(|| AppError::Key(format!("no invite key named {name}")))?;
            output.event(Event::KeyShown {
                name: &name,
                secret: &key.secret,
            })?;
            Ok(())
        }
    }
}

fn normalize_name(name: &str) -> Result<String, String> {
    let name = name.trim().to_ascii_lowercase();
    if valid_name(&name) {
        Ok(name)
    } else {
        Err(format!(
            "name must be 1 to {MAX_NAME_LENGTH} lowercase letters, numbers, or hyphens"
        ))
    }
}

fn parse_duration(input: &str) -> Result<u64, String> {
    let input = input.trim().to_ascii_lowercase();
    if input.len() < 2 {
        return Err("expires-in needs a value and unit, like 7d or 24h".into());
    }
    let (amount, unit) = input.split_at(input.len() - 1);
    let amount: u64 = amount
        .parse()
        .map_err(|_| format!("invalid duration {input:?}"))?;
    let multiplier = match unit {
        "s" => 1,
        "m" => 60,
        "h" => 60 * 60,
        "d" => 24 * 60 * 60,
        "w" => 7 * 24 * 60 * 60,
        _ => {
            return Err(format!(
                "invalid duration unit {unit:?}; use s, m, h, d, or w"
            ));
        }
    };
    amount
        .checked_mul(multiplier)
        .ok_or_else(|| format!("duration {input:?} is too large"))
}

fn random_secret() -> String {
    const ALPHABET: &[u8] = b"ABCDEFGHJKLMNPQRSTUVWXYZ23456789";
    let mut rng = rand::rng();
    let mut groups = Vec::with_capacity(3);
    for _ in 0..3 {
        let group: String = (0..4)
            .map(|_| ALPHABET[rng.random_range(0..ALPHABET.len())] as char)
            .collect();
        groups.push(group);
    }
    groups.join("-")
}

fn read_secret_line() -> Result<String, AppError> {
    let mut input = String::new();
    let mut bounded = std::io::Read::take(std::io::stdin().lock(), (MAX_SECRET_BYTES + 1) as u64);
    bounded
        .read_line(&mut input)
        .map_err(|error| AppError::Key(format!("could not read the secret from stdin: {error}")))?;
    if input.len() > MAX_SECRET_BYTES {
        return Err(AppError::Key("the secret from stdin is too long".into()));
    }
    let secret = input.strip_suffix('\n').unwrap_or(&input);
    let secret = secret.strip_suffix('\r').unwrap_or(secret);
    if secret.is_empty() {
        return Err(AppError::Key(
            "the secret stdin must contain one non-empty line".into(),
        ));
    }
    Ok(secret.to_string())
}

fn now_epoch() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_secs() as i64)
        .unwrap_or(0)
}

#[cfg(unix)]
fn write_private(path: &Path, content: &[u8]) -> Result<(), String> {
    use std::os::unix::fs::OpenOptionsExt;

    let mut file = OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(path)
        .map_err(|error| format!("could not create {}: {error}", path.display()))?;
    file.write_all(content)
        .and_then(|_| file.sync_all())
        .map_err(|error| format!("could not write {}: {error}", path.display()))
}

#[cfg(unix)]
fn check_private_permissions(path: &Path) -> Result<(), String> {
    use std::os::unix::fs::PermissionsExt;

    let mode = fs::metadata(path)
        .map_err(|error| format!("could not read {}: {error}", path.display()))?
        .permissions()
        .mode()
        & 0o777;
    if mode & 0o077 != 0 {
        return Err(format!(
            "{} is readable or writable by group or others (mode {mode:o}); \
             run chmod 600 {}",
            path.display(),
            path.display()
        ));
    }
    Ok(())
}

#[cfg(not(unix))]
fn check_private_permissions(_path: &Path) -> Result<(), String> {
    Ok(())
}

#[cfg(not(unix))]
fn write_private(path: &Path, content: &[u8]) -> Result<(), String> {
    let mut file = OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(path)
        .map_err(|error| format!("could not create {}: {error}", path.display()))?;
    file.write_all(content)
        .and_then(|_| file.sync_all())
        .map_err(|error| format!("could not write {}: {error}", path.display()))
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeMap;
    use std::path::PathBuf;

    use super::{InviteKey, InviteKeysFile, parse_duration};

    #[test]
    fn duration_parser_accepts_common_units() {
        assert_eq!(parse_duration("90s").unwrap(), 90);
        assert_eq!(parse_duration("30m").unwrap(), 30 * 60);
        assert_eq!(parse_duration("24h").unwrap(), 24 * 60 * 60);
        assert_eq!(parse_duration("7d").unwrap(), 7 * 24 * 60 * 60);
        assert_eq!(parse_duration("1w").unwrap(), 7 * 24 * 60 * 60);
        assert!(parse_duration("soon").is_err());
    }

    #[test]
    fn duplicate_secrets_are_rejected() {
        let file = InviteKeysFile {
            keys: BTreeMap::from([
                (
                    "alpha".into(),
                    InviteKey {
                        secret: "same-secret-123".into(),
                        ..Default::default()
                    },
                ),
                (
                    "beta".into(),
                    InviteKey {
                        secret: "same-secret-123".into(),
                        ..Default::default()
                    },
                ),
            ]),
        };

        assert!(file.validate().is_err());
    }

    #[test]
    fn default_key_allows_one_use() {
        assert_eq!(InviteKey::default().max_uses, 1);
    }

    #[test]
    fn short_secrets_are_rejected() {
        let file = InviteKeysFile {
            keys: BTreeMap::from([(
                "demo".into(),
                InviteKey {
                    secret: "short".into(),
                    ..Default::default()
                },
            )]),
        };

        assert!(file.validate().is_err());
    }

    #[test]
    fn missing_file_is_an_empty_key_set() {
        let path = PathBuf::from("/definitely/not/gnar/keys.json");
        assert!(InviteKeysFile::load(&path).unwrap().keys.is_empty());
    }
}
