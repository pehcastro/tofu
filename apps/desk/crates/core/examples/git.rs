use std::env;
use std::error::Error;
use std::fs;
use std::path::Path;
use std::process::{Command, ExitCode};

use desk_core::git::{Against, Git, GitBinary, State};

fn scratch(dir: &Path) -> Result<(), Box<dyn Error>> {
    if dir.exists() {
        fs::remove_dir_all(dir)?;
    }
    fs::create_dir_all(dir)?;
    let program = GitBinary::locate()?;
    let git = |args: &[&str]| -> Result<(), Box<dyn Error>> {
        let status = Command::new(&program)
            .args(args)
            .current_dir(dir)
            .status()?;
        if status.success() {
            Ok(())
        } else {
            Err(format!("git {} failed with {status}", args.join(" ")).into())
        }
    };
    git(&["init", "-q", "-b", "main"])?;
    git(&["config", "user.name", "Scratch"])?;
    git(&["config", "user.email", "scratch@example.invalid"])?;
    git(&["config", "core.autocrlf", "false"])?;
    fs::write(dir.join("notes.txt"), "one\ntwo\nthree\nfour\nfive\n")?;
    git(&["add", "notes.txt"])?;
    git(&["commit", "-q", "-m", "first"])?;
    fs::write(dir.join("notes.txt"), "one\nTWO\nthree\nfour\nfive\nsix\n")?;
    println!("scratch repo at {}", dir.display());
    Ok(())
}

fn describe(state: &State) -> String {
    match state {
        State::Tracked { index, worktree } => {
            format!("tracked index={index:?} worktree={worktree:?}")
        }
        State::Renamed {
            from,
            index,
            worktree,
        } => format!("renamed from {from} index={index:?} worktree={worktree:?}"),
        State::Conflicted(conflict) => format!("conflicted {conflict:?}"),
        State::Untracked => "untracked".to_owned(),
        State::Ignored => "ignored".to_owned(),
    }
}

fn run(args: &[String]) -> Result<(), Box<dyn Error>> {
    let [verb, repo, rest @ ..] = args else {
        return Err("usage: git <verb> <repo> [args]".into());
    };
    if verb == "scratch" {
        return scratch(Path::new(repo));
    }
    let git = GitBinary::open(Path::new(repo))?;
    match (verb.as_str(), rest) {
        ("status", []) => {
            let entries = git.status()?;
            println!("{} entries in {}", entries.len(), git.root().display());
            for entry in &entries {
                println!("{}  {}", describe(&entry.state), entry.path);
            }
        }
        ("diff", [path, against @ ..]) => {
            let against = match against {
                [] => Against::Index,
                [head] if head == "head" => Against::Head,
                _ => return Err("diff takes head or nothing after the path".into()),
            };
            for hunk in git.diff(path, against)? {
                println!(
                    "@@ -{},{} +{},{} @@ removed {:?} added {:?}",
                    hunk.old.start,
                    hunk.old.count,
                    hunk.new.start,
                    hunk.new.count,
                    hunk.removed,
                    hunk.added
                );
            }
        }
        ("blame", [path, buffer @ ..]) => {
            let contents = match buffer {
                [] => None,
                [file] => Some(fs::read_to_string(file)?),
                _ => return Err("blame takes at most one buffer file".into()),
            };
            for (number, line) in git.blame(path, contents.as_deref())?.iter().enumerate() {
                let commit = line
                    .commit
                    .as_deref()
                    .map_or("not committed", |sha| sha.get(..8).unwrap_or(sha));
                println!(
                    "{:>3} {commit:<13} {:<18} {:>10} {}",
                    number + 1,
                    line.author,
                    line.time,
                    line.text
                );
            }
        }
        ("stage", paths) => git.stage(paths)?,
        ("unstage", paths) => git.unstage(paths)?,
        ("discard", paths) => git.discard(paths)?,
        ("commit", [message]) => git.commit(message)?,
        ("branches", []) => {
            let current = git.current_branch()?;
            for branch in git.branches()? {
                let marker = if current.as_ref() == Some(&branch) {
                    "*"
                } else {
                    " "
                };
                println!("{marker} {branch}");
            }
        }
        ("switch", [branch]) => git.switch(branch)?,
        _ => return Err(format!("unknown verb or arguments: {verb}").into()),
    }
    Ok(())
}

fn main() -> ExitCode {
    let args: Vec<String> = env::args().skip(1).collect();
    match run(&args) {
        Ok(()) => ExitCode::SUCCESS,
        Err(error) => {
            eprintln!("{error}");
            ExitCode::FAILURE
        }
    }
}
