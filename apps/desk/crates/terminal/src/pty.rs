use flume::r#async::RecvStream;
use portable_pty::{ChildKiller, CommandBuilder, MasterPty, PtySize, native_pty_system};
use std::env;
use std::io::{ErrorKind, Read, Write};
use std::path::{Path, PathBuf};
use std::thread;

const READ_CHUNK: usize = 16 * 1024;
const OUTPUT_BACKLOG: usize = 32;
const TERM_PROGRAM: &str = "tofu-desk";
const POWERSHELLS: [&str; 2] = ["pwsh", "powershell"];

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

#[cfg(windows)]
pub(crate) fn pick_shell() -> PathBuf {
    let installed = env::var_os("ProgramFiles").map(|folder| {
        PathBuf::from(folder)
            .join("PowerShell")
            .join("7")
            .join("pwsh.exe")
    });
    let on_path = env::var_os("PATH").and_then(|paths| {
        env::split_paths(&paths)
            .map(|folder| folder.join("pwsh.exe"))
            .find(|candidate| candidate.is_file())
    });
    let windows = env::var_os("SystemRoot").map(|root| {
        PathBuf::from(root)
            .join("System32")
            .join("WindowsPowerShell")
            .join("v1.0")
            .join("powershell.exe")
    });
    [installed, on_path, windows]
        .into_iter()
        .flatten()
        .find(|candidate| candidate.is_file())
        .or_else(|| env::var_os("ComSpec").map(PathBuf::from))
        .unwrap_or_else(|| PathBuf::from("cmd.exe"))
}

#[cfg(not(windows))]
pub(crate) fn pick_shell() -> PathBuf {
    env::var_os("SHELL").map_or_else(|| PathBuf::from("/bin/sh"), PathBuf::from)
}

fn command(shell: &Path, cwd: &Path) -> CommandBuilder {
    let mut command = CommandBuilder::new(shell);
    let powershell = shell.file_stem().is_some_and(|stem| {
        POWERSHELLS
            .iter()
            .any(|name| stem.eq_ignore_ascii_case(name))
    });
    if powershell {
        command.arg("-NoLogo");
    }
    command.cwd(cwd);
    command.env("TERM", "xterm-256color");
    command.env("COLORTERM", "truecolor");
    command.env("TERM_PROGRAM", TERM_PROGRAM);
    command.env("TERM_PROGRAM_VERSION", env!("CARGO_PKG_VERSION"));
    command
}

impl Pty {
    pub(crate) fn spawn(
        shell: &Path,
        cwd: &Path,
        cols: u16,
        rows: u16,
    ) -> Result<(Self, RecvStream<'static, Output>), String> {
        let pair = native_pty_system()
            .openpty(size(cols, rows))
            .map_err(|error| format!("could not open a pty: {error:#}"))?;
        let mut child = pair
            .slave
            .spawn_command(command(shell, cwd))
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
