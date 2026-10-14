use desk_control::transport::{Client, instances};
use std::io::ErrorKind;
use std::path::Path;
use std::time::Duration;

const CONNECT_TIMEOUT: Duration = Duration::from_secs(2);

pub enum Found {
    Connected(Client),
    NotRunning,
}

pub fn connect() -> std::io::Result<Found> {
    for (path, instance) in instances()? {
        match Client::connect(&instance, CONNECT_TIMEOUT) {
            Ok(client) => return Ok(Found::Connected(client)),
            Err(e) if e.kind() == ErrorKind::ConnectionRefused => remove_stale(&path)?,
            Err(e) => return Err(e),
        }
    }
    Ok(Found::NotRunning)
}

fn remove_stale(path: &Path) -> std::io::Result<()> {
    match std::fs::remove_file(path) {
        Err(e) if e.kind() != ErrorKind::NotFound => Err(e),
        _ => Ok(()),
    }
}
