use crate::kept::Attachment;
use crate::pty::{ByteSink, Output};
use base64::Engine as _;
use base64::engine::general_purpose::STANDARD;
use flume::{Receiver, Sender};
use std::cell::Cell;
use std::iter;
use std::time::{Duration, Instant};

const INPUT_MAX_BYTES: usize = 65536;

#[derive(Clone, Copy)]
pub struct Key {
    pub seq: u64,
    pub down: Instant,
}

pub struct Echo {
    pub key: Key,
    pub batched: Duration,
    pub back: Duration,
}

pub struct Input {
    pub keys: Vec<Key>,
    pub data: Vec<String>,
}

struct WireSink {
    keys: Sender<(Key, Vec<u8>)>,
    sizes: Sender<(u16, u16)>,
    typed: Cell<u64>,
}

impl ByteSink for WireSink {
    fn send(&self, bytes: Vec<u8>) -> bool {
        let seq = self.typed.get().wrapping_add(1);
        self.typed.set(seq);
        let key = Key {
            seq,
            down: Instant::now(),
        };
        self.keys.send((key, bytes)).is_ok()
    }

    fn resize(&self, cols: u16, rows: u16) -> Result<(), String> {
        self.sizes
            .send((cols, rows))
            .map_err(|_| format!("the shell was handed back, so {cols}x{rows} went nowhere"))
    }
}

pub struct Wired {
    pub attachment: Attachment,
    pub keys: Receiver<(Key, Vec<u8>)>,
    pub sizes: Receiver<(u16, u16)>,
    pub output: Sender<Output>,
}

pub fn wired(replay: Vec<u8>) -> Wired {
    let (keys_in, keys) = flume::unbounded();
    let (sizes_in, sizes) = flume::unbounded();
    let (output, received) = flume::unbounded();
    Wired {
        attachment: Attachment {
            sink: Box::new(WireSink {
                keys: keys_in,
                sizes: sizes_in,
                typed: Cell::new(0),
            }),
            output: received,
            replay,
        },
        keys,
        sizes,
        output,
    }
}

pub fn decode(data: &str) -> Result<Vec<u8>, String> {
    STANDARD
        .decode(data)
        .map_err(|error| format!("tofu sent shell bytes that are not base64: {error}"))
}

pub async fn next_input(typed: &Receiver<(Key, Vec<u8>)>) -> Option<Input> {
    let first = typed.recv_async().await.ok()?;
    let (keys, bytes): (Vec<Key>, Vec<Vec<u8>>) = iter::once(first).chain(typed.drain()).unzip();
    Some(Input {
        keys,
        data: bytes
            .concat()
            .chunks(INPUT_MAX_BYTES)
            .map(|chunk| STANDARD.encode(chunk))
            .collect(),
    })
}

pub async fn next_size(sizes: &Receiver<(u16, u16)>) -> Option<(u16, u16)> {
    let first = sizes.recv_async().await.ok()?;
    Some(sizes.drain().last().unwrap_or(first))
}
