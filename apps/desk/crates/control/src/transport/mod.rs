use crate::CONTRACT;
use crate::envelope::{Failure, Id, JsonRpc, Request, Success, WireRequest, parse_line};
use crate::error::ErrorCode;
use crate::events::Feed;
use crate::params::{EventsSince, Initialize};
use crate::replies::{Following, Initialized, Pong};
use crate::verbs::Call;
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::collections::hash_map::RandomState;
use std::hash::{BuildHasher, Hasher};
use std::io::{self, BufRead, BufReader, Write};
use std::net::{SocketAddr, TcpListener, TcpStream};
use std::path::PathBuf;
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

pub type Reply = Result<Value, Failure>;
type Handler = dyn Fn(Request) -> Reply + Send + Sync;

const SUPPORTED_VERSION: u32 = 1;
const PROBE_TIMEOUT: Duration = Duration::from_millis(200);
const EVENT: &str = "events.event";
const RESYNC: &str = "events.resync";

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Instance {
    pub pid: u32,
    pub endpoint: String,
    pub token: String,
    pub contract: String,
    pub desk_version: String,
    pub project: Option<String>,
}

#[derive(Serialize, Deserialize)]
struct Hello {
    token: String,
}

#[derive(Serialize)]
struct Notice<'a, T> {
    jsonrpc: JsonRpc,
    method: &'a str,
    params: T,
}

struct Shared {
    instance: Instance,
    started: Instant,
    handler: Box<Handler>,
    feed: Feed,
}

pub struct Server {
    shared: Arc<Shared>,
    path: PathBuf,
    stop: Arc<AtomicBool>,
    accept: Option<JoinHandle<()>>,
}

impl Server {
    pub fn start(
        desk_version: &str,
        project: Option<&str>,
        handle: impl Fn(Request) -> Reply + Send + Sync + 'static,
    ) -> io::Result<Server> {
        let dir = runtime_dir().ok_or_else(|| io::Error::other("no home directory"))?;
        std::fs::create_dir_all(&dir)?;
        let listener = TcpListener::bind("127.0.0.1:0")?;
        let pid = std::process::id();
        let instance = Instance {
            pid,
            endpoint: listener.local_addr()?.to_string(),
            token: token(),
            contract: CONTRACT.to_owned(),
            desk_version: desk_version.to_owned(),
            project: project.map(str::to_owned),
        };
        let path = dir.join(format!("instance-{pid}.json"));
        std::fs::write(
            &path,
            serde_json::to_vec(&instance).map_err(io::Error::other)?,
        )?;
        let shared = Arc::new(Shared {
            instance,
            started: Instant::now(),
            handler: Box::new(handle),
            feed: Feed::default(),
        });
        let stop = Arc::new(AtomicBool::new(false));
        let accept = {
            let shared = Arc::clone(&shared);
            let stop = Arc::clone(&stop);
            thread::spawn(move || {
                for stream in listener.incoming() {
                    if stop.load(Ordering::SeqCst) {
                        break;
                    }
                    let Ok(stream) = stream else { continue };
                    let shared = Arc::clone(&shared);
                    thread::spawn(move || serve(&shared, stream));
                }
            })
        };
        Ok(Server {
            shared,
            path,
            stop,
            accept: Some(accept),
        })
    }

    pub fn instance(&self) -> &Instance {
        &self.shared.instance
    }

    pub fn feed(&self) -> Feed {
        self.shared.feed.clone()
    }
}

impl Drop for Server {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::SeqCst);
        if let Ok(addr) = self.shared.instance.endpoint.parse::<SocketAddr>()
            && TcpStream::connect_timeout(&addr, PROBE_TIMEOUT).is_ok()
            && let Some(accept) = self.accept.take()
            && accept.join().is_err()
        {
            eprintln!("desk control: accept thread panicked");
        }
        if let Err(e) = std::fs::remove_file(&self.path) {
            eprintln!(
                "desk control: could not remove {}: {e}",
                self.path.display()
            );
        }
    }
}

fn serve(shared: &Shared, stream: TcpStream) {
    let Ok(writer) = stream.try_clone() else {
        return;
    };
    let mut writer = writer;
    let mut lines = BufReader::new(stream).lines();
    let Some(Ok(first)) = lines.next() else {
        return;
    };
    match serde_json::from_str::<Hello>(&first) {
        Ok(hello) if hello.token == shared.instance.token => {}
        _ => return,
    }
    for line in lines {
        let Ok(line) = line else { return };
        let mut following = None;
        let out = match parse_line(&line) {
            Ok(Request {
                id,
                call: Call::EventsSubscribe(_),
            }) => {
                let head = shared.feed.head();
                following = Some(head);
                success(id, Following { head })
            }
            Ok(request) => {
                let id = request.id.clone();
                match answer(shared, request) {
                    Ok(result) => success(id, result),
                    Err(failure) => serde_json::to_string(&failure),
                }
            }
            Err(failure) => serde_json::to_string(&failure),
        };
        let Ok(out) = out else { return };
        if writeln!(writer, "{out}")
            .and_then(|()| writer.flush())
            .is_err()
        {
            return;
        }
        if let Some(head) = following {
            return follow(&shared.feed, &mut writer, head);
        }
    }
}

fn success(id: Id, result: impl Serialize) -> serde_json::Result<String> {
    serde_json::to_string(&Success {
        jsonrpc: JsonRpc::V2,
        id,
        result,
    })
}

fn follow(feed: &Feed, writer: &mut TcpStream, mut cursor: u64) {
    loop {
        let page = feed.after(cursor);
        cursor = page.head;
        let lines = match page.resync {
            true => vec![notice(RESYNC, &page)],
            false => page
                .events
                .iter()
                .map(|event| notice(EVENT, event))
                .collect(),
        };
        let sent = lines.into_iter().try_for_each(|line| {
            let line = line.map_err(io::Error::other)?;
            writeln!(writer, "{line}")
        });
        if sent.and_then(|()| writer.flush()).is_err() {
            return;
        }
    }
}

fn notice(method: &str, params: impl Serialize) -> serde_json::Result<String> {
    serde_json::to_string(&Notice {
        jsonrpc: JsonRpc::V2,
        method,
        params,
    })
}

fn answer(shared: &Shared, request: Request) -> Reply {
    match request.call {
        Call::EventsSince(EventsSince { cursor }) => {
            serde_json::to_value(shared.feed.since(cursor)).map_err(|e| internal(&request.id, &e))
        }
        Call::DeskPing(_) => {
            let uptime_ms = u64::try_from(shared.started.elapsed().as_millis()).unwrap_or(u64::MAX);
            serde_json::to_value(Pong { rev: 0, uptime_ms }).map_err(|e| internal(&request.id, &e))
        }
        Call::DeskInitialize(Initialize { versions, .. }) => {
            if !versions.contains(&SUPPORTED_VERSION) {
                return Err(Failure::new(
                    Some(request.id),
                    ErrorCode::UnsupportedVersion,
                    format!("this desk speaks version {SUPPORTED_VERSION}"),
                    Some("desk.initialize".to_owned()),
                ));
            }
            let instance = &shared.instance;
            serde_json::to_value(Initialized {
                version: SUPPORTED_VERSION,
                capabilities: Vec::new(),
                desk_version: instance.desk_version.clone(),
                pid: instance.pid,
                project: instance.project.clone().unwrap_or_default(),
                features: Vec::new(),
            })
            .map_err(|e| internal(&request.id, &e))
        }
        _ => (shared.handler)(request),
    }
}

fn internal(id: &Id, e: &serde_json::Error) -> Failure {
    Failure::new(Some(id.clone()), ErrorCode::Refused, e.to_string(), None)
}

fn token() -> String {
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |d| d.as_nanos());
    (0..4u8).fold(String::with_capacity(64), |mut out, round| {
        let mut hasher = RandomState::new().build_hasher();
        hasher.write_u128(nanos);
        hasher.write_u32(std::process::id());
        hasher.write_u8(round);
        out.push_str(&format!("{:016x}", hasher.finish()));
        out
    })
}

pub fn runtime_dir() -> Option<PathBuf> {
    let var = if cfg!(windows) { "USERPROFILE" } else { "HOME" };
    let home = std::env::var_os(var)?;
    Some(
        PathBuf::from(home)
            .join(".tofu")
            .join("desk")
            .join("runtime"),
    )
}

pub fn instances() -> io::Result<Vec<(PathBuf, Instance)>> {
    let Some(dir) = runtime_dir() else {
        return Ok(Vec::new());
    };
    let entries = match std::fs::read_dir(&dir) {
        Ok(entries) => entries,
        Err(e) if e.kind() == io::ErrorKind::NotFound => return Ok(Vec::new()),
        Err(e) => return Err(e),
    };
    let mut found = Vec::new();
    for entry in entries {
        let path = entry?.path();
        let named = path
            .file_name()
            .and_then(|n| n.to_str())
            .is_some_and(|n| n.starts_with("instance-") && n.ends_with(".json"));
        if !named {
            continue;
        }
        let Ok(instance) = serde_json::from_slice::<Instance>(&std::fs::read(&path)?) else {
            continue;
        };
        if alive(&instance) {
            found.push((path, instance));
        } else {
            std::fs::remove_file(&path)?;
        }
    }
    Ok(found)
}

fn alive(instance: &Instance) -> bool {
    instance
        .endpoint
        .parse::<SocketAddr>()
        .is_ok_and(|addr| TcpStream::connect_timeout(&addr, PROBE_TIMEOUT).is_ok())
}

pub struct Client {
    writer: TcpStream,
    reader: BufReader<TcpStream>,
}

impl Client {
    pub fn connect(instance: &Instance, timeout: Duration) -> io::Result<Client> {
        let addr: SocketAddr = instance
            .endpoint
            .parse()
            .map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))?;
        let mut writer = TcpStream::connect_timeout(&addr, timeout)?;
        writer.set_read_timeout(Some(timeout))?;
        writer.set_write_timeout(Some(timeout))?;
        writer.set_nodelay(true)?;
        let hello = serde_json::to_string(&Hello {
            token: instance.token.clone(),
        })
        .map_err(io::Error::other)?;
        writeln!(writer, "{hello}")?;
        writer.flush()?;
        let reader = BufReader::new(writer.try_clone()?);
        Ok(Client { writer, reader })
    }

    pub fn call(&mut self, request: &WireRequest) -> io::Result<Reply> {
        let line = serde_json::to_string(request).map_err(io::Error::other)?;
        writeln!(self.writer, "{line}")?;
        self.writer.flush()?;
        let mut reply = String::new();
        if self.reader.read_line(&mut reply)? == 0 {
            return Err(io::Error::new(io::ErrorKind::ConnectionAborted, "closed"));
        }
        let value: Value = serde_json::from_str(&reply)
            .map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))?;
        if value.get("error").is_some() {
            let failure: Failure = serde_json::from_value(value)
                .map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))?;
            return Ok(Err(failure));
        }
        let success: Success<Value> = serde_json::from_value(value)
            .map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))?;
        Ok(Ok(success.result))
    }

    pub fn listen(&mut self, mut each: impl FnMut(&str) -> io::Result<()>) -> io::Result<()> {
        self.reader.get_ref().set_read_timeout(None)?;
        let mut line = String::new();
        loop {
            line.clear();
            if self.reader.read_line(&mut line)? == 0 {
                return Ok(());
            }
            each(line.trim_end())?;
        }
    }
}
