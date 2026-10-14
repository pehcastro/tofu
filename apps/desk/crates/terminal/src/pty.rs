use crate::shells;
use flume::Receiver;
use portable_pty::{ChildKiller, CommandBuilder, MasterPty, PtySize, native_pty_system};
use std::env;
use std::fs::{self, File, OpenOptions};
use std::io::{self, ErrorKind, Read, Write};
use std::path::Path;
use std::rc::Rc;
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::thread;
use std::time::Instant;

const READ_CHUNK: usize = 16 * 1024;
const TERM_PROGRAM: &str = "tofu-desk";
const BYTES_LOG_VARIABLE: &str = "DESK_TERMINAL_BYTES";
const BYTES_LOG_FILE: &str = "terminal-bytes.log";
const POWERSHELLS: [&str; 2] = ["pwsh", "powershell"];

pub enum Output {
    Bytes(Vec<u8>),
    Exited(Option<u32>),
}

pub trait ByteSink {
    fn send(&self, bytes: Vec<u8>) -> bool;
    fn resize(&self, cols: u16, rows: u16) -> Result<(), String>;
}

pub(crate) struct Pty {
    master: Box<dyn MasterPty + Send>,
    input: flume::Sender<Vec<u8>>,
    killer: Box<dyn ChildKiller + Send + Sync>,
    pid: Option<u32>,
    exited: Arc<AtomicBool>,
}

fn command(shell: &Path, cwd: &Path) -> CommandBuilder {
    let mut command = CommandBuilder::new(shell);
    if POWERSHELLS.contains(&shells::name(shell).as_str()) {
        command.arg("-NoLogo");
    }
    command.cwd(cwd);
    command.env("TERM", "xterm-256color");
    command.env("COLORTERM", "truecolor");
    command.env("TERM_PROGRAM", TERM_PROGRAM);
    command.env("TERM_PROGRAM_VERSION", env!("CARGO_PKG_VERSION"));
    command
}

fn bytes_log() -> Option<File> {
    env::var_os(BYTES_LOG_VARIABLE)?;
    let Some(folder) = shells::desk_folder() else {
        eprintln!("terminal: {BYTES_LOG_VARIABLE} is set but there is no config folder");
        return None;
    };
    let path = folder.join(BYTES_LOG_FILE);
    let opened = fs::create_dir_all(&folder)
        .and_then(|()| OpenOptions::new().create(true).append(true).open(&path));
    match opened {
        Ok(file) => {
            eprintln!("terminal: pty bytes go to {}", path.display());
            Some(file)
        }
        Err(error) => {
            eprintln!("terminal: could not open {}: {error}", path.display());
            None
        }
    }
}

fn log_bytes(log: &mut Option<File>, started: Instant, bytes: &[u8]) {
    let Some(file) = log else {
        return;
    };
    let at = started.elapsed().as_millis();
    if let Err(error) = writeln!(file, "{at} {}", bytes.escape_ascii()) {
        eprintln!("terminal: the pty byte log stopped: {error}");
        *log = None;
    }
}

impl Pty {
    pub(crate) fn spawn(
        shell: &Path,
        args: &[&str],
        cwd: &Path,
        (cols, rows): (u16, u16),
    ) -> Result<(Self, Receiver<Output>), String> {
        let pair = native_pty_system()
            .openpty(size(cols, rows))
            .map_err(|error| format!("could not open a pty: {error:#}"))?;
        let mut built = command(shell, cwd);
        for arg in args {
            built.arg(arg);
        }
        let mut child = pair
            .slave
            .spawn_command(built)
            .map_err(|error| format!("could not start {}: {error:#}", shell.display()))?;
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
        let (output, received) = flume::unbounded();
        let exits = output.clone();
        let mut log = bytes_log();
        thread::spawn(move || {
            let started = Instant::now();
            let mut chunk = vec![0; READ_CHUNK];
            loop {
                match reader.read(&mut chunk) {
                    Ok(0) => return,
                    Ok(read) => {
                        let bytes = chunk.get(..read).unwrap_or_default().to_vec();
                        log_bytes(&mut log, started, &bytes);
                        if output.send(Output::Bytes(bytes)).is_err() {
                            return;
                        }
                    }
                    Err(error) if error.kind() == ErrorKind::Interrupted => {}
                    Err(_) => return,
                }
            }
        });
        let exited = Arc::new(AtomicBool::new(false));
        let ended = exited.clone();
        thread::spawn(move || {
            let code = child.wait().ok().map(|status| status.exit_code());
            ended.store(true, Ordering::Release);
            exits.send(Output::Exited(code))
        });
        Ok((
            Pty {
                master: pair.master,
                input,
                killer,
                pid,
                exited,
            },
            received,
        ))
    }

    pub(crate) fn pid(&self) -> Option<u32> {
        self.pid
    }

    pub(crate) fn input(&self) -> flume::Sender<Vec<u8>> {
        self.input.clone()
    }

    pub(crate) fn alive(&self) -> bool {
        !self.exited.load(Ordering::Acquire)
    }

    fn kill(&mut self) -> Result<(), String> {
        if !self.alive() {
            return Ok(());
        }
        terminated(self.killer.kill())
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

impl ByteSink for Rc<Pty> {
    fn send(&self, bytes: Vec<u8>) -> bool {
        Pty::send(self, bytes)
    }

    fn resize(&self, cols: u16, rows: u16) -> Result<(), String> {
        Pty::resize(self, cols, rows)
    }
}

impl Drop for Pty {
    fn drop(&mut self) {
        if let Err(error) = self.kill() {
            eprintln!("{error}");
        }
    }
}

#[cfg(windows)]
fn terminated(said: io::Result<()>) -> Result<(), String> {
    match said {
        Err(_) => Ok(()),
        Ok(()) => Err("could not kill the shell: TerminateProcess refused".to_owned()),
    }
}

#[cfg(not(windows))]
fn terminated(said: io::Result<()>) -> Result<(), String> {
    said.map_err(|error| format!("could not kill the shell: {error:#}"))
}

fn size(cols: u16, rows: u16) -> PtySize {
    PtySize {
        rows,
        cols,
        pixel_width: 0,
        pixel_height: 0,
    }
}
