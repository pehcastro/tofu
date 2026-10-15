mod capture;
mod change;
mod dispatch;
#[cfg(feature = "plugin-draw")]
mod draw;
mod editor;
mod tabs;
mod workspace;

use std::io;
use std::time::{Duration, Instant};

use desk_control::transport::{Reply, Server};
use desk_control::{ErrorCode, Failure, Request};
use gpui::{AnyWindowHandle, App};

use crate::project;

const BUSY_AFTER: Duration = Duration::from_secs(2);

pub struct Asked {
    request: Request,
    reply: flume::Sender<Reply>,
}

pub fn start() -> io::Result<(Server, flume::Receiver<Asked>)> {
    let (ask, asked) = flume::unbounded();
    let project = project::launch()
        .ok()
        .map(|folder| folder.to_string_lossy().into_owned());
    let server = Server::start(
        env!("CARGO_PKG_VERSION"),
        project.as_deref(),
        move |request| forward(&ask, request),
    )?;
    Ok((server, asked))
}

fn forward(ask: &flume::Sender<Asked>, request: Request) -> Reply {
    let started = Instant::now();
    let verb = dispatch::verb(&request.call);
    let id = request.id.clone();
    let (reply, answer) = flume::bounded(1);
    let answered = ask
        .send(Asked { request, reply })
        .ok()
        .and_then(|()| answer.recv_timeout(BUSY_AFTER).ok());
    eprintln!("desk: control {verb} {} ms", started.elapsed().as_millis());
    answered.unwrap_or_else(|| {
        Err(Failure::new(
            Some(id),
            ErrorCode::Busy,
            format!(
                "the desk did not answer within {} ms",
                BUSY_AFTER.as_millis()
            ),
            Some(verb.to_owned()),
        ))
    })
}

pub fn serve(
    asked: flume::Receiver<Asked>,
    window: AnyWindowHandle,
    screens: Vec<(&'static str, bool)>,
    cx: &mut App,
) {
    cx.spawn(async move |cx| {
        let mut journal = Vec::new();
        while let Ok(Asked { request, reply }) = asked.recv_async().await {
            let verb = dispatch::verb(&request.call);
            let answer =
                cx.update(|cx| dispatch::answer(request, window, &screens, &mut journal, cx));
            match &answer {
                Ok(_) => eprintln!("desk: control {verb} ok"),
                Err(failure) => eprintln!("desk: control {verb} refused {}", failure.error.message),
            }
            if reply.send(answer).is_err() {
                eprintln!("desk: control: the caller stopped waiting before the answer");
            }
        }
    })
    .detach();
}
