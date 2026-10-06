use std::error::Error;
use std::path::Path;
use std::process::Command;
use std::time::Duration;

use desk_core::bridge::{Bridge, BridgeError, Event, serve_command};
use desk_core::protocol::{
    Notification, PROTOCOL, SessionOpenParams, TurnCompletedStatus, TurnSendParams, request,
};

const TURN_DEADLINE: Duration = Duration::from_secs(60);

fn tofu_replaying(scratch: &Path, cassette: &str) -> Result<Command, Box<dyn Error>> {
    let project = scratch.join("project");
    let home = scratch.join("home");
    std::fs::create_dir_all(&project)?;
    std::fs::create_dir_all(&home)?;
    let mut command = serve_command(Path::new("tofu"), &project);
    command
        .arg("--cassette")
        .arg(
            Path::new(env!("CARGO_MANIFEST_DIR"))
                .join("tests/cassettes")
                .join(cassette),
        )
        .env("USERPROFILE", &home)
        .env("HOME", &home);
    Ok(command)
}

#[test]
fn a_turn_replayed_from_a_cassette_arrives_typed_and_ends_finished() -> Result<(), Box<dyn Error>> {
    let scratch = tempfile::tempdir()?;
    let (bridge, hello) = Bridge::open(
        tofu_replaying(scratch.path(), "hi.cassette")?,
        "desk-test",
        PROTOCOL,
    )?;
    assert_eq!(hello.protocol, PROTOCOL);

    let opened = bridge
        .request::<request::SessionOpen>(&SessionOpenParams::default())?
        .wait()?;
    assert!(opened.fresh);
    let sent = bridge
        .request::<request::TurnSend>(&TurnSendParams {
            session: opened.session.clone(),
            text: "say hi".to_owned(),
            ..TurnSendParams::default()
        })?
        .wait()?;

    let mut said = String::new();
    let mut seqs = Vec::new();
    let done = loop {
        match bridge.events().recv_timeout(TURN_DEADLINE)? {
            Event::Notification(Notification::MessageDelta(delta)) => {
                assert_eq!(delta.turn, sent.turn);
                seqs.push(delta.seq);
                said.push_str(&delta.text);
            }
            Event::Notification(Notification::TurnStarted(started)) => {
                assert_eq!(started.session, opened.session);
                seqs.push(started.seq);
            }
            Event::Notification(Notification::TurnCompleted(done)) => break done,
            Event::Notification(Notification::Unknown { method, .. }) => {
                return Err(
                    format!("tofu sent {method}, which the pinned schema does not name").into(),
                );
            }
            Event::Unreadable { line, reason } => {
                return Err(format!("unreadable line {line}: {reason}").into());
            }
            Event::Notification(_) | Event::Request { .. } => {}
        }
    };
    assert_eq!(done.turn, sent.turn);
    assert_eq!(done.session, opened.session);
    assert_eq!(done.status, TurnCompletedStatus::Finished);
    assert_eq!(said, "hi");
    assert!(
        seqs.windows(2).all(|pair| pair[0] < pair[1]),
        "seq went backwards: {seqs:?}"
    );

    let exit = bridge.stop()?;
    assert!(
        exit.success(),
        "tofu did not stop cleanly on a closed stdin: {exit}"
    );
    Ok(())
}

#[test]
fn a_tofu_that_does_not_speak_the_needed_protocol_is_refused_by_name() -> Result<(), Box<dyn Error>>
{
    let scratch = tempfile::tempdir()?;
    let needed = "tofu.host/2";
    let refused = match Bridge::open(
        tofu_replaying(scratch.path(), "hi.cassette")?,
        "desk-test",
        needed,
    ) {
        Ok(_) => {
            return Err(
                "a tofu speaking tofu.host/1 was accepted by a desk that needs tofu.host/2".into(),
            );
        }
        Err(refused) => refused,
    };
    assert!(
        matches!(refused, BridgeError::Protocol { .. }),
        "{refused:?}"
    );
    assert!(refused.to_string().contains(needed), "{refused}");
    Ok(())
}

#[test]
fn the_pinned_schema_is_the_one_the_installed_tofu_prints() -> Result<(), Box<dyn Error>> {
    let printed = Command::new("tofu").args(["serve", "--schema"]).output()?;
    assert!(
        printed.status.success(),
        "tofu serve --schema exited {}",
        printed.status
    );
    let pinned: serde_json::Value = serde_json::from_str(include_str!("../schema/tofu.host.json"))?;
    let live: serde_json::Value = serde_json::from_slice(&printed.stdout)?;
    assert!(
        live == pinned,
        "tofu serve --schema differs from crates/core/schema/tofu.host.json: regenerate it"
    );
    Ok(())
}
