use std::collections::HashMap;
use std::env;
use std::io::{self, Write};
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::sync::OnceLock;
use std::thread;

use super::{Against, BlameLine, Drift, Entry, Git, GitError, Hunk, parse};

#[cfg(windows)]
const CREATE_NO_WINDOW: u32 = 0x0800_0000;

pub struct GitBinary {
    program: PathBuf,
    root: PathBuf,
    names: OnceLock<HashMap<String, String>>,
    own_email: OnceLock<Option<String>>,
}

fn run_in(
    program: &Path,
    dir: &Path,
    args: &[&str],
    input: Option<&str>,
) -> Result<String, GitError> {
    let mut command = Command::new(program);
    command
        .args(args)
        .current_dir(dir)
        .env("GIT_OPTIONAL_LOCKS", "0")
        .stdin(if input.is_some() {
            Stdio::piped()
        } else {
            Stdio::null()
        })
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
    #[cfg(windows)]
    std::os::windows::process::CommandExt::creation_flags(&mut command, CREATE_NO_WINDOW);
    let mut child = command.spawn().map_err(GitError::Spawn)?;
    let stdin = child.stdin.take();
    let (output, written) = thread::scope(|scope| {
        let writer = scope.spawn(move || match (stdin, input) {
            (Some(mut stdin), Some(text)) => stdin.write_all(text.as_bytes()),
            _ => Ok(()),
        });
        let output = child.wait_with_output();
        let written = writer
            .join()
            .unwrap_or_else(|_| Err(io::Error::other("the stdin writer panicked")));
        (output, written)
    });
    let output = output.map_err(GitError::Spawn)?;
    if output.status.success() {
        written.map_err(GitError::Spawn)?;
    } else {
        return Err(GitError::Failed {
            args: args.iter().map(|&arg| arg.to_owned()).collect(),
            code: output.status.code(),
            stderr: String::from_utf8_lossy(&output.stderr).into_owned(),
        });
    }
    String::from_utf8(output.stdout).map_err(|error| GitError::Parse {
        what: "output that is not UTF-8",
        text: String::from_utf8_lossy(error.as_bytes()).into_owned(),
    })
}

impl GitBinary {
    pub fn locate() -> Result<PathBuf, GitError> {
        let name = if cfg!(windows) { "git.exe" } else { "git" };
        env::var_os("PATH")
            .iter()
            .flat_map(env::split_paths)
            .map(|path| path.join(name))
            .find(|candidate| candidate.is_file())
            .ok_or_else(|| {
                GitError::Spawn(io::Error::new(
                    io::ErrorKind::NotFound,
                    "git is not on PATH",
                ))
            })
    }

    pub fn open(dir: &Path) -> Result<GitBinary, GitError> {
        let program = GitBinary::locate()?;
        let top = run_in(&program, dir, &["rev-parse", "--show-toplevel"], None)?;
        Ok(GitBinary {
            program,
            root: PathBuf::from(top.trim_end()),
            names: OnceLock::new(),
            own_email: OnceLock::new(),
        })
    }

    pub fn root(&self) -> &Path {
        &self.root
    }

    pub fn own_email(&self) -> Result<Option<&str>, GitError> {
        if let Some(own) = self.own_email.get() {
            return Ok(own.as_deref());
        }
        let own = match self.run(&["config", "user.email"], None) {
            Ok(email) => Some(email.trim().to_lowercase()).filter(|email| !email.is_empty()),
            Err(GitError::Failed { code: Some(1), .. }) => None,
            Err(error) => return Err(error),
        };
        Ok(self.own_email.get_or_init(|| own).as_deref())
    }

    fn names(&self) -> Result<&HashMap<String, String>, GitError> {
        if let Some(names) = self.names.get() {
            return Ok(names);
        }
        let names = parse::shortlog(&self.run(&["shortlog", "-sne", "HEAD"], None)?)?;
        Ok(self.names.get_or_init(|| names))
    }

    pub fn drift(&self) -> Result<Option<Drift>, GitError> {
        parse::drift(&self.run(&["status", "--porcelain=v2", "--branch", "-uno"], None)?)
    }

    fn run(&self, args: &[&str], input: Option<&str>) -> Result<String, GitError> {
        run_in(&self.program, &self.root, args, input)
    }

    fn on_paths(&self, args: &[&str], paths: &[String]) -> Result<(), GitError> {
        let mut all = args.to_vec();
        all.push("--");
        all.extend(paths.iter().map(String::as_str));
        self.run(&all, None).map(drop)
    }
}

impl Git for GitBinary {
    fn status(&self) -> Result<Vec<Entry>, GitError> {
        parse::status(&self.run(
            &["status", "--porcelain=v2", "-z", "--ignored=matching"],
            None,
        )?)
    }

    fn diff(&self, path: &str, against: Against) -> Result<Vec<Hunk>, GitError> {
        let mut args = vec!["diff", "-U0", "--no-color", "--no-ext-diff"];
        if against == Against::Head {
            args.push("HEAD");
        }
        args.extend(["--", path]);
        parse::diff(&self.run(&args, None)?)
    }

    fn blame(&self, path: &str, contents: Option<&str>) -> Result<Vec<BlameLine>, GitError> {
        let mut args = vec!["blame", "--porcelain"];
        if contents.is_some() {
            args.extend(["--contents", "-"]);
        }
        args.extend(["--", path]);
        let lines = parse::blame(&self.run(&args, contents)?)?;
        let names = self.names()?;
        Ok(lines
            .into_iter()
            .map(|line| match names.get(&line.email) {
                Some(name) => BlameLine {
                    author: name.clone(),
                    ..line
                },
                None => line,
            })
            .collect())
    }

    fn stage(&self, paths: &[String]) -> Result<(), GitError> {
        self.on_paths(&["add"], paths)
    }

    fn unstage(&self, paths: &[String]) -> Result<(), GitError> {
        self.on_paths(&["restore", "--staged"], paths)
    }

    fn discard(&self, paths: &[String]) -> Result<(), GitError> {
        self.on_paths(&["restore"], paths)
    }

    fn commit(&self, message: &str) -> Result<(), GitError> {
        self.run(&["commit", "-q", "-F", "-"], Some(message))
            .map(drop)
    }

    fn branches(&self) -> Result<Vec<String>, GitError> {
        let out = self.run(
            &["for-each-ref", "--format=%(refname:short)", "refs/heads"],
            None,
        )?;
        Ok(out.lines().map(str::to_owned).collect())
    }

    fn current_branch(&self) -> Result<Option<String>, GitError> {
        match self.run(&["symbolic-ref", "--short", "-q", "HEAD"], None) {
            Ok(name) => Ok(Some(name.trim_end().to_owned())),
            Err(GitError::Failed { code: Some(1), .. }) => Ok(None),
            Err(error) => Err(error),
        }
    }

    fn switch(&self, branch: &str) -> Result<(), GitError> {
        self.run(&["switch", "-q", branch], None).map(drop)
    }
}
