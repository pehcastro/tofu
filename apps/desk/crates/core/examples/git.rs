use std::env;
use std::error::Error;
use std::fs;
use std::path::Path;
use std::process::{Command, ExitCode};

use desk_core::git::{Against, Commit, DiffOf, Git, GitBinary, PullHow, Side, State};

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

fn print_commits(commits: &[Commit]) {
    for commit in commits {
        let refs: Vec<String> = commit
            .refs
            .iter()
            .map(|r| format!("{:?}:{}", r.kind, r.name))
            .collect();
        let parents: Vec<&str> = commit
            .parents
            .iter()
            .map(|p| p.get(..8).unwrap_or(p))
            .collect();
        println!(
            "{} [{}] {} <{}> {} {} [{}]",
            commit.sha.get(..8).unwrap_or(&commit.sha),
            parents.join(" "),
            commit.author,
            commit.email,
            commit.when,
            commit.subject,
            refs.join(", ")
        );
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
        ("log", [skip, count]) => print_commits(&git.log(skip.parse()?, count.parse()?)?),
        ("show", [sha]) => {
            let detail = git.show(sha)?;
            print_commits(std::slice::from_ref(&detail.commit));
            println!("body: {:?}", detail.body);
            for file in &detail.files {
                println!(
                    "{:?} +{} -{} {}{}",
                    file.mark,
                    file.added,
                    file.removed,
                    file.from
                        .as_deref()
                        .map_or_else(String::new, |from| format!("{from} -> ")),
                    file.path
                );
            }
        }
        ("file_diff", [path, of @ ..]) => {
            let of = match of {
                [] => DiffOf::Worktree,
                [index] if index == "index" => DiffOf::Index,
                [sha] => DiffOf::Commit(sha.clone()),
                _ => return Err("file_diff takes index, a sha or nothing after the path".into()),
            };
            print!("{}", git.file_diff(path, &of)?);
        }
        ("branches_full", []) => {
            for b in git.branches_full()? {
                println!(
                    "{} {} upstream={:?} ahead={} behind={} tip={}",
                    if b.current { "*" } else { " " },
                    b.name,
                    b.upstream,
                    b.ahead,
                    b.behind,
                    b.tip.get(..8).unwrap_or(&b.tip)
                );
            }
        }
        ("remotes", []) => {
            for (name, url) in git.remotes()? {
                println!("{name} {url}");
            }
        }
        ("tags", []) => println!("{}", git.tags()?.join("\n")),
        ("stashes", []) => {
            for stash in git.stashes()? {
                println!("stash@{{{}}} {} {}", stash.index, stash.when, stash.message);
            }
        }
        ("undo", [count]) => {
            for step in git.undo_steps(count.parse()?)? {
                println!(
                    "{} {} {}",
                    step.sha.get(..8).unwrap_or(&step.sha),
                    step.when,
                    step.action
                );
            }
        }
        ("operation", []) => println!("{:?}", git.operation()?),
        ("stage_patch", [file, reverse @ ..]) => {
            git.stage_patch(&fs::read_to_string(file)?, !reverse.is_empty())?;
        }
        ("discard_patch", [file]) => git.discard_patch(&fs::read_to_string(file)?)?,
        ("commit_with", [message, amend @ ..]) => git.commit_with(message, !amend.is_empty())?,
        ("create_branch", [name, at]) => git.create_branch(name, at)?,
        ("delete_branch", [name]) => git.delete_branch(name)?,
        ("fetch", []) => git.fetch()?,
        ("pull", [how]) => git.pull(match how.as_str() {
            "merge" => PullHow::Merge,
            "rebase" => PullHow::Rebase,
            _ => return Err("pull takes merge or rebase".into()),
        })?,
        ("push", lease) => git.push(!lease.is_empty())?,
        ("outgoing", []) => print_commits(&git.outgoing()?),
        ("stash_push", [message]) => git.stash_push(message)?,
        ("stash_pop", [index]) => git.stash_pop(index.parse()?)?,
        ("stash_drop", [index]) => git.stash_drop(index.parse()?)?,
        ("restore_to", [sha, keep @ ..]) => git.restore_to(sha, !keep.is_empty())?,
        ("take_side", [path, side]) => git.take_side(
            path,
            match side.as_str() {
                "ours" => Side::Ours,
                "theirs" => Side::Theirs,
                _ => return Err("take_side takes ours or theirs".into()),
            },
        )?,
        ("mark_resolved", [path]) => git.mark_resolved(path)?,
        ("continue", []) => git.continue_operation()?,
        ("abort", []) => git.abort_operation()?,
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
