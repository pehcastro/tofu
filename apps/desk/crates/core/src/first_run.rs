use std::fmt;
use std::path::Path;

use crate::bridge::{Bridge, BridgeError, Pending, serve_command};
use crate::protocol::{NoParams, PROTOCOL, Requirement, request};

const CLIENT: &str = "tofu-desk-first-run";
const ACCOUNT_STEP: &str = "language model";
const CLASSIFIER_STEP: &str = "classifier · jev, required";

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum FirstRun {
    NoTofu { reason: String },
    OldTofu { found: String },
    NoProject,
    NoAccount,
    NoClassifier,
    UnknownStep { step: String },
    Ready,
}

impl fmt::Display for FirstRun {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            FirstRun::NoTofu { .. } => "NoTofu",
            FirstRun::OldTofu { .. } => "OldTofu",
            FirstRun::NoProject => "NoProject",
            FirstRun::NoAccount => "NoAccount",
            FirstRun::NoClassifier => "NoClassifier",
            FirstRun::UnknownStep { .. } => "UnknownStep",
            FirstRun::Ready => "Ready",
        })
    }
}

pub fn detect(tofu: &Path, project: Option<&Path>) -> FirstRun {
    let bridge = match Bridge::open(
        serve_command(tofu, project.unwrap_or(Path::new("."))),
        CLIENT,
        PROTOCOL,
    ) {
        Ok((bridge, _)) => bridge,
        Err(BridgeError::Io(error)) => {
            return FirstRun::NoTofu {
                reason: error.to_string(),
            };
        }
        Err(refused) => {
            return FirstRun::OldTofu {
                found: refused.to_string(),
            };
        }
    };
    let state = match project {
        None => FirstRun::NoProject,
        Some(_) => match bridge
            .request::<request::SetupCheck>(&NoParams {})
            .and_then(Pending::wait)
        {
            Ok(setup) => first_undone(&setup.steps),
            Err(refused) => FirstRun::OldTofu {
                found: refused.to_string(),
            },
        },
    };
    if let Err(error) = bridge.stop() {
        eprintln!("desk: first run could not stop tofu: {error}");
    }
    state
}

fn first_undone(steps: &[Requirement]) -> FirstRun {
    let undone = steps
        .iter()
        .find(|step| step.done.as_deref().is_none_or(str::is_empty));
    match undone.map(|step| step.step.as_str()) {
        None => FirstRun::Ready,
        Some(ACCOUNT_STEP) => FirstRun::NoAccount,
        Some(CLASSIFIER_STEP) => FirstRun::NoClassifier,
        Some(step) => FirstRun::UnknownStep {
            step: step.to_owned(),
        },
    }
}
