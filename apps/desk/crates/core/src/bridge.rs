use std::collections::{HashMap, VecDeque};
use std::fmt;
use std::io::{self, BufRead, BufReader, Read, Write};
use std::marker::PhantomData;
use std::path::Path;
use std::process::{Child, ChildStdin, Command, ExitStatus, Stdio};
use std::sync::atomic::{AtomicI64, Ordering};
use std::sync::{Arc, Mutex, PoisonError};
use std::thread;
use std::time::Instant;

use serde::de::DeserializeOwned;
use serde::{Deserialize, Serialize};
use serde_json::Value;

use crate::limits;
use crate::protocol::{
    ApprovalAnswer, InitializeParams, InitializeResult, Notification, Refusal, Request, RequestId,
    ServerRequest, request,
};

#[cfg(windows)]
const CREATE_NO_WINDOW: u32 = 0x0800_0000;

type Reply = Result<Value, Refusal>;
type Waiting = Arc<Mutex<Option<HashMap<i64, flume::Sender<Reply>>>>>;
type Log = Arc<Mutex<VecDeque<String>>>;

#[derive(Debug)]
pub enum Event {
    Notification(Notification),
    Request {
        id: RequestId,
        request: ServerRequest,
    },
    Unreadable {
        line: String,
        reason: String,
    },
}

#[derive(Debug)]
pub enum BridgeError {
    Io(io::Error),
    Closed,
    Refused(Refusal),
    Shape(serde_json::Error),
    Protocol { needed: String, detail: String },
}

impl fmt::Display for BridgeError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            BridgeError::Io(error) => write!(f, "tofu could not be run: {error}"),
            BridgeError::Closed => write!(f, "tofu closed its output before it answered"),
            BridgeError::Refused(refusal) => {
                write!(
                    f,
                    "tofu refused the request: {} ({})",
                    refusal.message, refusal.code
                )
            }
            BridgeError::Shape(error) => {
                write!(
                    f,
                    "a message does not match the pinned tofu.host schema: {error}"
                )
            }
            BridgeError::Protocol { needed, detail } => {
                write!(
                    f,
                    "this desk needs the protocol {needed}, and this tofu does not speak it: {detail}"
                )
            }
        }
    }
}

impl std::error::Error for BridgeError {}

impl From<io::Error> for BridgeError {
    fn from(error: io::Error) -> Self {
        BridgeError::Io(error)
    }
}

impl From<serde_json::Error> for BridgeError {
    fn from(error: serde_json::Error) -> Self {
        BridgeError::Shape(error)
    }
}

pub struct Pending<T> {
    answer: flume::Receiver<Reply>,
    shape: PhantomData<fn() -> T>,
}

impl<T: DeserializeOwned> Pending<T> {
    pub fn wait(self) -> Result<T, BridgeError> {
        decode(self.answer.recv())
    }

    pub async fn reply(self) -> Result<T, BridgeError> {
        decode(self.answer.recv_async().await)
    }
}

fn decode<T: DeserializeOwned>(answer: Result<Reply, flume::RecvError>) -> Result<T, BridgeError> {
    let value = answer
        .map_err(|_| BridgeError::Closed)?
        .map_err(BridgeError::Refused)?;
    Ok(serde_json::from_value(value)?)
}

#[derive(Serialize)]
struct Outgoing<'a, P> {
    jsonrpc: &'static str,
    id: i64,
    method: &'static str,
    params: &'a P,
}

#[derive(Serialize)]
struct Answer<'a> {
    jsonrpc: &'static str,
    id: &'a RequestId,
    result: &'a ApprovalAnswer,
}

#[derive(Deserialize)]
struct Incoming {
    id: Option<RequestId>,
    method: Option<String>,
    #[serde(default)]
    params: Value,
    result: Option<Value>,
    error: Option<Refusal>,
}

pub struct Bridge {
    child: Child,
    lines: flume::Sender<String>,
    waiting: Waiting,
    next: AtomicI64,
    events: flume::Receiver<Event>,
    log: Log,
    #[cfg(windows)]
    _job: win32job::Job,
}

pub fn serve_command(tofu: &Path, project: &Path) -> Command {
    let mut command = Command::new(tofu);
    command.args(["serve", "--stdio"]).current_dir(project);
    command
}

impl Bridge {
    pub fn open(
        mut command: Command,
        client: &str,
        needed: &str,
    ) -> Result<(Bridge, InitializeResult), BridgeError> {
        command
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        #[cfg(windows)]
        std::os::windows::process::CommandExt::creation_flags(&mut command, CREATE_NO_WINDOW);
        let mut child = command.spawn()?;
        #[cfg(windows)]
        let job = match contain(&child) {
            Ok(job) => job,
            Err(failed) => {
                child.kill()?;
                return Err(BridgeError::Io(io::Error::other(failed)));
            }
        };
        let (Some(stdin), Some(stdout), Some(stderr)) =
            (child.stdin.take(), child.stdout.take(), child.stderr.take())
        else {
            child.kill()?;
            return Err(BridgeError::Io(io::Error::other(
                "tofu started without its pipes",
            )));
        };

        let (lines, queued) = flume::unbounded();
        thread::spawn(move || write_lines(stdin, &queued));
        let waiting: Waiting = Arc::new(Mutex::new(Some(HashMap::new())));
        let (sender, events) = flume::bounded(limits::BRIDGE_EVENT_QUEUE);
        let routing = Arc::clone(&waiting);
        thread::spawn(move || read_stdout(stdout, &routing, &sender));
        let log: Log = Arc::default();
        let kept = Arc::clone(&log);
        thread::spawn(move || read_stderr(stderr, &kept));

        let bridge = Bridge {
            child,
            lines,
            waiting,
            next: AtomicI64::new(1),
            events,
            log,
            #[cfg(windows)]
            _job: job,
        };
        let hello = bridge
            .request::<request::Initialize>(&InitializeParams {
                client: client.to_owned(),
                versions: vec![needed.to_owned()],
                capabilities: Vec::new(),
            })?
            .wait();
        let refused = |detail: String| BridgeError::Protocol {
            needed: needed.to_owned(),
            detail,
        };
        let hello = match hello {
            Err(BridgeError::Refused(refusal)) => return Err(refused(refusal.message)),
            other => other?,
        };
        if hello.protocol != needed {
            return Err(refused(format!(
                "tofu {} speaks {}",
                hello.tofu, hello.protocol
            )));
        }
        Ok((bridge, hello))
    }

    pub fn request<R: Request>(
        &self,
        params: &R::Params,
    ) -> Result<Pending<R::Result>, BridgeError> {
        let id = self.next.fetch_add(1, Ordering::Relaxed);
        let line = serde_json::to_string(&Outgoing {
            jsonrpc: "2.0",
            id,
            method: R::METHOD,
            params,
        })?;
        let (reply, answer) = flume::bounded(1);
        self.waiting
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .as_mut()
            .ok_or(BridgeError::Closed)?
            .insert(id, reply);
        self.lines.send(line).map_err(|_| BridgeError::Closed)?;
        Ok(Pending {
            answer,
            shape: PhantomData,
        })
    }

    pub fn answer(&self, id: &RequestId, answer: &ApprovalAnswer) -> Result<(), BridgeError> {
        let line = serde_json::to_string(&Answer {
            jsonrpc: "2.0",
            id,
            result: answer,
        })?;
        self.lines.send(line).map_err(|_| BridgeError::Closed)
    }

    pub fn events(&self) -> &flume::Receiver<Event> {
        &self.events
    }

    pub fn log(&self) -> Vec<String> {
        self.log
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .iter()
            .cloned()
            .collect()
    }

    pub fn pid(&self) -> u32 {
        self.child.id()
    }

    pub fn stop(mut self) -> Result<ExitStatus, BridgeError> {
        drop(self.lines);
        let deadline = Instant::now() + limits::TOFU_STOP_GRACE;
        while Instant::now() < deadline {
            if let Some(status) = self.child.try_wait()? {
                return Ok(status);
            }
            thread::sleep(limits::TOFU_STOP_POLL);
        }
        self.child.kill()?;
        Ok(self.child.wait()?)
    }
}

#[cfg(windows)]
fn contain(child: &Child) -> Result<win32job::Job, win32job::JobError> {
    use std::os::windows::io::AsRawHandle;
    let mut info = win32job::ExtendedLimitInfo::new();
    info.limit_kill_on_job_close();
    let job = win32job::Job::create_with_limit_info(&info)?;
    job.assign_process(child.as_raw_handle() as isize)?;
    Ok(job)
}

fn write_lines(mut stdin: ChildStdin, queued: &flume::Receiver<String>) {
    for line in queued.iter() {
        if writeln!(stdin, "{line}")
            .and_then(|()| stdin.flush())
            .is_err()
        {
            return;
        }
    }
}

fn lossy_lines(reader: impl Read) -> impl Iterator<Item = String> {
    let mut reader = BufReader::new(reader);
    std::iter::from_fn(move || {
        let mut line = Vec::new();
        match reader.read_until(b'\n', &mut line) {
            Ok(0) | Err(_) => None,
            Ok(_) => Some(String::from_utf8_lossy(&line).trim_end().to_owned()),
        }
    })
}

fn read_stdout(stdout: impl Read, waiting: &Waiting, events: &flume::Sender<Event>) {
    for line in lossy_lines(stdout) {
        let event = match route(&line, waiting) {
            Ok(None) => continue,
            Ok(Some(event)) => event,
            Err(reason) => Event::Unreadable { line, reason },
        };
        if events.send(event).is_err() {
            break;
        }
    }
    waiting
        .lock()
        .unwrap_or_else(PoisonError::into_inner)
        .take();
}

fn route(line: &str, waiting: &Waiting) -> Result<Option<Event>, String> {
    let incoming: Incoming = serde_json::from_str(line).map_err(|error| error.to_string())?;
    let unshaped = |error: serde_json::Error| error.to_string();
    match (
        incoming.method,
        incoming.id,
        incoming.result,
        incoming.error,
    ) {
        (Some(method), Some(id), _, _) => Ok(Some(Event::Request {
            id,
            request: ServerRequest::parse(&method, incoming.params).map_err(unshaped)?,
        })),
        (Some(method), None, _, _) => Ok(Some(Event::Notification(
            Notification::parse(&method, incoming.params).map_err(unshaped)?,
        ))),
        (None, Some(RequestId::Number(id)), result, error) => {
            let reply = match (result, error) {
                (_, Some(refusal)) => Err(refusal),
                (Some(result), None) => Ok(result),
                (None, None) => return Err("a response with neither a result nor an error".into()),
            };
            let waiter = waiting
                .lock()
                .unwrap_or_else(PoisonError::into_inner)
                .as_mut()
                .and_then(|waiting| waiting.remove(&id))
                .ok_or(format!(
                    "a response to request {id}, which nobody is waiting on"
                ))?;
            match waiter.send(reply) {
                Ok(()) | Err(flume::SendError(_)) => Ok(None),
            }
        }
        (None, id, _, error) => Err(format!(
            "a response to no request of this desk: {id:?} {error:?}"
        )),
    }
}

fn read_stderr(stderr: impl Read, log: &Log) {
    for line in lossy_lines(stderr) {
        let mut log = log.lock().unwrap_or_else(PoisonError::into_inner);
        if log.len() == limits::TOFU_LOG_LINES {
            log.pop_front();
        }
        log.push_back(line);
    }
}
