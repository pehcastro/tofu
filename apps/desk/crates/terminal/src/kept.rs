use crate::pty::{ByteSink, Output, Pty};
use crate::{INITIAL_COLS, INITIAL_ROWS};
use flume::{Receiver, Sender};
use std::path::Path;
use std::rc::Rc;
use std::sync::{Arc, Mutex, MutexGuard, PoisonError};
use std::thread;

const REPLAY_CAP: usize = 8 * 1024 * 1024;
const CURSOR_QUERY: &[u8] = b"\x1b[6n";
const CURSOR_AT_ORIGIN: &[u8] = b"\x1b[1;1R";

#[derive(Default)]
struct Held {
    replay: Vec<u8>,
    tile: Option<Sender<Output>>,
}

pub struct Kept {
    pty: Rc<Pty>,
    held: Arc<Mutex<Held>>,
}

pub struct Attachment {
    pub sink: Box<dyn ByteSink>,
    pub output: Receiver<Output>,
    pub replay: Vec<u8>,
}

fn lock(held: &Mutex<Held>) -> MutexGuard<'_, Held> {
    held.lock().unwrap_or_else(PoisonError::into_inner)
}

impl Kept {
    pub fn spawn(program: &Path, args: &[&str], cwd: &Path) -> Result<Self, String> {
        let (pty, output) = Pty::spawn(program, args, cwd, (INITIAL_COLS, INITIAL_ROWS))?;
        let held = Arc::new(Mutex::new(Held::default()));
        let forwarded = held.clone();
        let input = pty.input();
        thread::spawn(move || {
            for output in output {
                let mut held = lock(&forwarded);
                if let Output::Bytes(bytes) = &output {
                    held.replay.extend_from_slice(bytes);
                    let over = held.replay.len().saturating_sub(REPLAY_CAP);
                    held.replay.drain(..over);
                    let asked = bytes
                        .windows(CURSOR_QUERY.len())
                        .filter(|window| *window == CURSOR_QUERY)
                        .count();
                    if held.tile.is_none()
                        && asked > 0
                        && input.send(CURSOR_AT_ORIGIN.repeat(asked)).is_err()
                    {
                        eprintln!(
                            "terminal: the kept shell's input closed before its cursor reply"
                        );
                    }
                }
                if held
                    .tile
                    .as_ref()
                    .is_some_and(|tile| tile.send(output).is_err())
                {
                    held.tile = None;
                }
            }
        });
        Ok(Kept {
            pty: Rc::new(pty),
            held,
        })
    }

    pub fn attach(&self) -> Attachment {
        let (tile, output) = flume::unbounded();
        let mut held = lock(&self.held);
        held.tile = Some(tile);
        Attachment {
            sink: Box::new(self.pty.clone()),
            output,
            replay: held.replay.clone(),
        }
    }

    pub fn release(&self) {
        lock(&self.held).tile = None;
    }

    pub fn pid(&self) -> Option<u32> {
        self.pty.pid()
    }

    pub fn alive(&self) -> bool {
        self.pty.alive()
    }
}
