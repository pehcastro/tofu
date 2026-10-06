use std::error::Error;
use std::path::Path;

use desk_core::bridge::{Bridge, serve_command};
use desk_core::protocol::{PROTOCOL, SessionOpenParams, TurnSendParams, request};

fn main() -> Result<(), Box<dyn Error>> {
    let arguments: Vec<String> = std::env::args().skip(1).collect();
    let (needed, cassette) = match arguments.as_slice() {
        [] => (PROTOCOL, None),
        [flag, protocol] if flag == "--needs" => (protocol.as_str(), None),
        [flag, path] if flag == "--cassette" => (PROTOCOL, Some(std::path::absolute(path)?)),
        _ => return Err("usage: bridge [--needs PROTOCOL | --cassette PATH]".into()),
    };
    let scratch = std::env::temp_dir().join(format!("desk-bridge-{}", std::process::id()));
    let (project, home) = (scratch.join("project"), scratch.join("home"));
    std::fs::create_dir_all(&project)?;
    std::fs::create_dir_all(&home)?;
    let mut command = serve_command(Path::new("tofu"), &project);
    command.env("USERPROFILE", &home).env("HOME", &home);
    if let Some(cassette) = &cassette {
        command.arg("--cassette").arg(cassette);
    }

    let (bridge, hello) = match Bridge::open(command, "desk-example", needed) {
        Ok(opened) => opened,
        Err(refused) => {
            eprintln!("{refused}");
            std::process::exit(1);
        }
    };
    println!(
        "desk pid {} holds tofu {} pid {} speaking {} in {}",
        std::process::id(),
        hello.tofu,
        bridge.pid(),
        hello.protocol,
        hello.project
    );
    let opened = bridge
        .request::<request::SessionOpen>(&SessionOpenParams::default())?
        .wait()?;
    println!(
        "session {} open, waiting for events until tofu or this desk ends",
        opened.session
    );
    if cassette.is_some() {
        let params = TurnSendParams {
            session: opened.session,
            text: "go".to_owned(),
            ..TurnSendParams::default()
        };
        bridge.request::<request::TurnSend>(&params)?.wait()?;
    }
    for event in bridge.events().iter() {
        println!("{event:?}");
    }
    println!(
        "tofu closed its output; its last log lines: {:?}",
        bridge.log()
    );
    Ok(())
}
