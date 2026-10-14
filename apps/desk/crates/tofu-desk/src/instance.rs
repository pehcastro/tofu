use desk_control::transport::{Client, Instance, instances};
use std::env::VarError;
use std::io::{Error, ErrorKind};
use std::path::Path;
use std::time::Duration;

const CONNECT_TIMEOUT: Duration = Duration::from_secs(2);
pub const PICK: &str = "TOFU_DESK_PID";

pub enum Found {
    Connected(Client),
    NotRunning,
    Several(Vec<Instance>),
}

pub fn connect() -> std::io::Result<Found> {
    let wanted = match std::env::var(PICK) {
        Ok(raw) => Some(raw.trim().parse::<u32>().map_err(|e| {
            Error::new(
                ErrorKind::InvalidInput,
                format!("{PICK}={raw} is not a pid: {e}"),
            )
        })?),
        Err(VarError::NotPresent) => None,
        Err(e) => return Err(Error::new(ErrorKind::InvalidInput, format!("{PICK}: {e}"))),
    };
    let mut live: Vec<_> = instances()?
        .into_iter()
        .filter(|(_, instance)| wanted.is_none_or(|pid| pid == instance.pid))
        .collect();
    if live.len() > 1 {
        return Ok(Found::Several(
            live.into_iter().map(|(_, instance)| instance).collect(),
        ));
    }
    let Some((path, instance)) = live.pop() else {
        return Ok(Found::NotRunning);
    };
    match Client::connect(&instance, CONNECT_TIMEOUT) {
        Ok(client) => Ok(Found::Connected(client)),
        Err(e) if e.kind() == ErrorKind::ConnectionRefused => {
            remove_stale(&path)?;
            Ok(Found::NotRunning)
        }
        Err(e) => Err(e),
    }
}

fn remove_stale(path: &Path) -> std::io::Result<()> {
    match std::fs::remove_file(path) {
        Err(e) if e.kind() != ErrorKind::NotFound => Err(e),
        _ => Ok(()),
    }
}
