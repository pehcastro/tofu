mod instance;

use desk_control::transport::{Client, Reply};
use desk_control::{ErrorCode, Id, JsonRpc, Method, Success, WireRequest};
use instance::Found;
use schemars::SchemaGenerator;
use serde_json::{Map, Value, json};
use std::io::{ErrorKind, Write};
use std::process::ExitCode;
use std::time::{SystemTime, UNIX_EPOCH};

enum Exit {
    Done,
    Failure,
    Usage,
    NotRunning,
    Denied,
    Timeout,
}

impl Exit {
    fn code(self) -> ExitCode {
        ExitCode::from(match self {
            Exit::Done => 0,
            Exit::Failure => 1,
            Exit::Usage => 2,
            Exit::NotRunning => 3,
            Exit::Denied => 4,
            Exit::Timeout => 5,
        })
    }
}

struct Outcome {
    exit: Exit,
    code: &'static str,
    message: String,
    hint: String,
}

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let json = args.iter().any(|a| a == "--json");
    let args: Vec<&str> = args
        .iter()
        .map(String::as_str)
        .filter(|a| *a != "--json")
        .collect();
    let Some((&verb, rest)) = args.split_first() else {
        return print_plain(&verb_groups(), Exit::Usage);
    };
    if verb == "schema" {
        return match serde_json::to_string_pretty(&desk_control::schema()) {
            Ok(text) => print_plain(&text, Exit::Done),
            Err(e) => print_plain(&e.to_string(), Exit::Failure),
        };
    }
    let (verb, rest) = alias(verb, rest);
    let (out, rest): (Vec<&str>, Vec<&str>) = rest
        .iter()
        .copied()
        .partition(|arg| verb == "capture" && arg.ends_with(".png"));
    let (line, exit) =
        match run(&verb, &rest, json).and_then(|reply| saved(reply, out.first().copied())) {
            Ok(reply) => replied(reply, json),
            Err(outcome) if json => (envelope(&verb, &outcome).to_string(), outcome.exit),
            Err(o) => (format!("✗ {}: {} → {}", o.code, o.message, o.hint), o.exit),
        };
    print_plain(&line, exit)
}

fn alias<'a>(verb: &str, rest: &'a [&'a str]) -> (String, &'a [&'a str]) {
    let known = |name: &str| Method::ALL.iter().any(|m| m.name() == name);
    let mut joined = verb.to_owned();
    let mut tail = rest;
    while !known(&joined) {
        let Some((next, after)) = tail.split_first() else {
            return (verb.to_owned(), rest);
        };
        joined = format!("{joined}.{next}");
        tail = after;
    }
    (joined, tail)
}

fn saved(reply: Reply, out: Option<&str>) -> Result<Reply, Outcome> {
    match (reply, out) {
        (Ok(mut result), Some(out)) => {
            let taken = result["path"].as_str().unwrap_or_default().to_owned();
            std::fs::copy(&taken, out).map_err(|e| Outcome {
                exit: Exit::Failure,
                code: "capture_unsaved",
                message: format!("{taken} did not copy to {out}: {e}"),
                hint: "check the folder exists and is writable".to_owned(),
            })?;
            result["path"] = Value::String(out.to_owned());
            Ok(Ok(result))
        }
        (reply, _) => Ok(reply),
    }
}

fn replied(reply: Reply, json: bool) -> (String, Exit) {
    let (text, exit) = match reply {
        Ok(result) if json => (
            serde_json::to_string(&Success {
                jsonrpc: JsonRpc::V2,
                id: Id::Number(1),
                result,
            }),
            Exit::Done,
        ),
        Ok(result) => (serde_json::to_string_pretty(&result), Exit::Done),
        Err(failure) => {
            let exit = match failure.error.data.kind {
                ErrorCode::DeniedByConsent | ErrorCode::DeniedByGate | ErrorCode::Refused => {
                    Exit::Denied
                }
                ErrorCode::Busy | ErrorCode::Timeout => Exit::Timeout,
                _ => Exit::Failure,
            };
            if json {
                (serde_json::to_string(&failure), exit)
            } else {
                let kind = failure.error.data.kind.name();
                (Ok(format!("✗ {kind}: {}", failure.error.message)), exit)
            }
        }
    };
    match text {
        Ok(text) => (text, exit),
        Err(e) => (e.to_string(), Exit::Failure),
    }
}

fn io_outcome(e: &std::io::Error) -> Outcome {
    let (exit, code, hint) = match e.kind() {
        ErrorKind::TimedOut | ErrorKind::WouldBlock => (
            Exit::Timeout,
            ErrorCode::Timeout.name(),
            "the desk did not answer in time",
        ),
        ErrorKind::ConnectionReset
        | ErrorKind::ConnectionAborted
        | ErrorKind::UnexpectedEof
        | ErrorKind::PermissionDenied => (
            Exit::Denied,
            ErrorCode::Refused.name(),
            "the desk closed the connection: its token may have changed",
        ),
        ErrorKind::InvalidInput => (
            Exit::Usage,
            ErrorCode::BadParams.name(),
            "TOFU_DESK_PID takes the pid of a running desk",
        ),
        _ => (
            Exit::Failure,
            "instance_unreadable",
            "check ~/.tofu/desk/runtime",
        ),
    };
    Outcome {
        exit,
        code,
        message: e.to_string(),
        hint: hint.to_owned(),
    }
}

fn run(verb: &str, rest: &[&str], json: bool) -> Result<Reply, Outcome> {
    let Some(&method) = Method::ALL.iter().find(|m| m.name() == verb) else {
        return Err(Outcome {
            exit: Exit::Usage,
            code: ErrorCode::UnknownMethod.name(),
            message: format!("unknown verb `{verb}`"),
            hint: "run tofu-desk with no arguments for the verbs".to_owned(),
        });
    };
    let (fields, required) = fields(method);
    let usage = usage(method, &fields, &required);
    let params = params(&fields, rest).map_err(|m| usage_outcome(m, usage.clone()))?;
    method
        .call(params.clone())
        .map_err(|e| usage_outcome(e.to_string(), usage))?;
    let mut client = match instance::connect().map_err(|e| io_outcome(&e))? {
        Found::Connected(client) => client,
        Found::NotRunning => {
            return Err(Outcome {
                exit: Exit::NotRunning,
                code: ErrorCode::DeskNotRunning.name(),
                message: "no desk is running".to_owned(),
                hint: format!(
                    "start the desk, or check {} names a running desk",
                    instance::PICK
                ),
            });
        }
        Found::Several(running) => {
            let listed: Vec<String> = running
                .iter()
                .map(|desk| {
                    format!(
                        "pid {} ({})",
                        desk.pid,
                        desk.project.as_deref().unwrap_or("no project")
                    )
                })
                .collect();
            return Err(Outcome {
                exit: Exit::Usage,
                code: "several_desks",
                message: format!("{} desks are running: {}", running.len(), listed.join(", ")),
                hint: format!("set {} to the pid of the one to drive", instance::PICK),
            });
        }
    };
    let (wire, params) = method.wire(params);
    let request = WireRequest {
        jsonrpc: JsonRpc::V2,
        id: Id::Number(1),
        method: wire.to_owned(),
        params: Some(params),
    };
    let reply = client.call(&request).map_err(|e| io_outcome(&e))?;
    match (method, reply) {
        (Method::EventsSubscribe, Ok(head)) => follow(client, head, json),
        (_, reply) => Ok(reply),
    }
}

fn follow(mut client: Client, head: Value, json: bool) -> Result<Reply, Outcome> {
    let mut out = std::io::stdout().lock();
    let (first, _) = replied(Ok(head), json);
    writeln!(out, "{first}")
        .and_then(|()| out.flush())
        .map_err(|e| io_outcome(&e))?;
    client
        .listen(|line| {
            let shown = match json {
                true => line.to_owned(),
                false => serde_json::from_str::<Value>(line)
                    .map_or_else(|_| line.to_owned(), |notice| notice["params"].to_string()),
            };
            writeln!(out, "{shown}").and_then(|()| out.flush())
        })
        .map_err(|e| io_outcome(&e))?;
    Err(Outcome {
        exit: Exit::NotRunning,
        code: ErrorCode::DeskNotRunning.name(),
        message: "the desk closed the event stream".to_owned(),
        hint: "start the desk, then catch up with tofu-desk events.since <cursor>".to_owned(),
    })
}

fn usage_outcome(message: String, usage: String) -> Outcome {
    Outcome {
        exit: Exit::Usage,
        code: ErrorCode::BadParams.name(),
        message,
        hint: usage,
    }
}

fn fields(method: Method) -> (Vec<String>, Vec<String>) {
    let mut generator = SchemaGenerator::default();
    let described = method.describe(&mut generator);
    let defs = generator.take_definitions(true);
    let mut schema = &described["params"];
    if let Some(name) = schema["$ref"].as_str().and_then(|r| r.rsplit('/').next()) {
        schema = defs.get(name).unwrap_or(&Value::Null);
    }
    let names = |key: &str| -> Vec<String> {
        match &schema[key] {
            Value::Object(map) => map.keys().cloned().collect(),
            Value::Array(list) => list
                .iter()
                .filter_map(|v| v.as_str().map(str::to_owned))
                .collect(),
            _ => Vec::new(),
        }
    };
    (names("properties"), names("required"))
}

fn params(fields: &[String], rest: &[&str]) -> Result<Value, String> {
    let mut params = Map::new();
    let mut positional = fields.iter();
    let mut rest = rest.iter();
    while let Some(&arg) = rest.next() {
        let (name, raw) = match arg.strip_prefix("--") {
            Some(name) => {
                let raw = rest.next().ok_or(format!("--{name} needs a value"))?;
                (name.to_owned(), *raw)
            }
            None => {
                let name = positional
                    .find(|f| !params.contains_key(*f))
                    .ok_or(format!("unexpected argument `{arg}`"))?;
                (name.clone(), arg)
            }
        };
        let piped = match (name.as_str(), raw) {
            ("args", "-") => Some(
                std::io::read_to_string(std::io::stdin())
                    .map_err(|e| format!("--args - did not read stdin: {e}"))?,
            ),
            _ => None,
        };
        let raw = piped.as_deref().unwrap_or(raw);
        let value = serde_json::from_str(raw).unwrap_or_else(|_| Value::String(raw.to_owned()));
        match value {
            Value::Object(whole) if name == "args" && !fields.iter().any(|f| f == "args") => {
                params.extend(whole)
            }
            value if name == "args" && piped.is_some() && !value.is_object() => {
                return Err("--args - needs a JSON object on stdin".to_owned());
            }
            value => {
                params.insert(name, value);
            }
        }
    }
    Ok(Value::Object(params))
}

fn usage(method: Method, fields: &[String], required: &[String]) -> String {
    let mut line = format!("tofu-desk {}", method.name());
    for field in fields {
        if required.contains(field) {
            line.push_str(&format!(" <{field}>"));
        } else {
            line.push_str(&format!(" [--{field} v]"));
        }
    }
    line.push_str(" [--json]");
    line
}

fn verb_groups() -> String {
    let mut groups: Vec<(&str, Vec<&str>)> = Vec::new();
    for method in Method::ALL {
        let name = method.name();
        let group = name.split('.').next().unwrap_or(name);
        match groups.iter_mut().find(|(g, _)| *g == group) {
            Some((_, verbs)) => verbs.push(name),
            None => groups.push((group, vec![name])),
        }
    }
    let mut text = "usage: tofu-desk <verb> [args] [--json]\n  schema".to_owned();
    for (group, verbs) in groups {
        text.push_str(&format!("\n  {group}: {}", verbs.join(" ")));
    }
    text
}

fn envelope(verb: &str, outcome: &Outcome) -> Value {
    let at = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |d| d.as_millis());
    json!({
        "tofu": "desk",
        "verb": verb,
        "ok": false,
        "at": at,
        "data": null,
        "problems": [{"code": outcome.code, "message": outcome.message, "hint": outcome.hint}],
    })
}

fn print_plain(text: &str, exit: Exit) -> ExitCode {
    let mut out = std::io::stdout().lock();
    match writeln!(out, "{text}").and_then(|()| out.flush()) {
        Ok(()) => exit.code(),
        Err(_) => Exit::Failure.code(),
    }
}
