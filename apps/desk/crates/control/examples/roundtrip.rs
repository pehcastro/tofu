use desk_control::transport::{Client, Instance, Server};
use desk_control::{ErrorCode, Failure, Id, JsonRpc, WireRequest};
use std::io;
use std::time::{Duration, Instant};

const PINGS: usize = 1000;
const TIMEOUT: Duration = Duration::from_secs(2);

fn main() -> io::Result<()> {
    let server = Server::start(env!("CARGO_PKG_VERSION"), None, |request| {
        Err(Failure::new(
            Some(request.id),
            ErrorCode::NotFound,
            "no handler".to_owned(),
            None,
        ))
    })?;
    let mut client = Client::connect(server.instance(), TIMEOUT)?;
    let mut times = Vec::with_capacity(PINGS);
    for n in 0..PINGS {
        let request = WireRequest {
            jsonrpc: JsonRpc::V2,
            id: Id::Number(i64::try_from(n).map_err(io::Error::other)?),
            method: "desk.ping".to_owned(),
            params: None,
        };
        let started = Instant::now();
        let reply = client.call(&request)?;
        times.push(started.elapsed());
        if let Err(failure) = reply {
            return Err(io::Error::other(failure.error.message));
        }
    }
    times.sort();
    let median = times.get(PINGS / 2).copied().unwrap_or_default();
    let p99 = times.get(PINGS * 99 / 100).copied().unwrap_or_default();
    println!("pings {PINGS} median {median:?} p99 {p99:?}");

    let forged = Instance {
        token: "0".repeat(64),
        ..server.instance().clone()
    };
    let mut intruder = Client::connect(&forged, TIMEOUT)?;
    let ping = WireRequest {
        jsonrpc: JsonRpc::V2,
        id: Id::Number(0),
        method: "desk.ping".to_owned(),
        params: None,
    };
    match intruder.call(&ping) {
        Ok(_) => println!("bad token: answered"),
        Err(e)
            if matches!(
                e.kind(),
                io::ErrorKind::ConnectionAborted
                    | io::ErrorKind::ConnectionReset
                    | io::ErrorKind::BrokenPipe
            ) =>
        {
            println!("bad token: closed")
        }
        Err(e) => println!("bad token: {e}"),
    }
    Ok(())
}
