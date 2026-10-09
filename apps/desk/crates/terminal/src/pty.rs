use flume::r#async::RecvStream;
use portable_pty::{ChildKiller, CommandBuilder, MasterPty, PtySize, native_pty_system};
use std::io::{ErrorKind, Read, Write};
use std::path::Path;
use std::thread;

const READ_CHUNK: usize = 16 * 1024;
const OUTPUT_BACKLOG: usize = 32;

pub(crate) enum Output {
    Bytes(Vec<u8>),
    Exited(Option<u32>),
}

pub(crate) struct Pty {
    master: Box<dyn MasterPty + Send>,
    input: flume::Sender<Vec<u8>>,
    killer: Box<dyn ChildKiller + Send + Sync>,
    pid: Option<u32>,
}

impl Pty {
    pub(crate) fn spawn(
        cwd: &Path,
        cols: u16,
        rows: u16,
    ) -> Result<(Self, RecvStream<'static, Output>), String> {
        let pair = native_pty_system()
            .openpty(size(cols, rows))
            .map_err(|error| format!("could not open a pty: {error:#}"))?;
        let mut command = CommandBuilder::new_default_prog();
        command.cwd(cwd);
        command.env("TERM", "xterm-256color");
        let mut child = pair
            .slave
            .spawn_command(command)
            .map_err(|error| format!("could not start the shell: {error:#}"))?;
        drop(pair.slave);
        let killer = child.clone_killer();
        let pid = child.process_id();
        let mut reader = pair
            .master
            .try_clone_reader()
            .map_err(|error| format!("could not read the pty: {error:#}"))?;
        let mut writer = pair
            .master
            .take_writer()
            .map_err(|error| format!("could not write the pty: {error:#}"))?;
        let (input, pending) = flume::unbounded::<Vec<u8>>();
        thread::spawn(move || {
            for bytes in pending {
                if writer
                    .write_all(&bytes)
                    .and_then(|()| writer.flush())
                    .is_err()
                {
                    return;
                }
            }
        });
        let (output, received) = flume::bounded(OUTPUT_BACKLOG);
        let exits = output.clone();
        thread::spawn(move || {
            let mut chunk = vec![0; READ_CHUNK];
            loop {
                match reader.read(&mut chunk) {
                    Ok(0) => return,
                    Ok(read) => {
                        let bytes = chunk.get(..read).unwrap_or_default().to_vec();
                        if output.send(Output::Bytes(bytes)).is_err() {
                            return;
                        }
                    }
                    Err(error) if error.kind() == ErrorKind::Interrupted => {}
                    Err(_) => return,
                }
            }
        });
        thread::spawn(move || {
            let code = child.wait().ok().map(|status| status.exit_code());
            exits.send(Output::Exited(code))
        });
        Ok((
            Pty {
                master: pair.master,
                input,
                killer,
                pid,
            },
            received.into_stream(),
        ))
    }

    pub(crate) fn pid(&self) -> Option<u32> {
        self.pid
    }

    pub(crate) fn kill(&mut self) -> Result<(), String> {
        self.killer
            .kill()
            .map_err(|error| format!("could not kill the shell: {error:#}"))
    }

    pub(crate) fn send(&self, bytes: Vec<u8>) -> bool {
        self.input.send(bytes).is_ok()
    }

    pub(crate) fn resize(&self, cols: u16, rows: u16) -> Result<(), String> {
        self.master
            .resize(size(cols, rows))
            .map_err(|error| format!("could not resize the pty: {error:#}"))
    }
}

impl Drop for Pty {
    fn drop(&mut self) {
        if let Err(error) = self.kill() {
            eprintln!("{error}");
        }
    }
}

fn size(cols: u16, rows: u16) -> PtySize {
    PtySize {
        rows,
        cols,
        pixel_width: 0,
        pixel_height: 0,
    }
}
