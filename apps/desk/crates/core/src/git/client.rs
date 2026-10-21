use std::fs;
use std::io;
use std::path::Path;

use super::{GitBinary, GitError, Mark, parse};

const COMMIT: &str = "--format=%H%x00%P%x00%an%x00%ae%x00%at%x00%s%x00%D";
const DETAIL: &str = "--format=%H%x00%P%x00%an%x00%ae%x00%at%x00%s%x00%D%x00%b";
const BRANCH: &str = "--format=%(refname:short)%00%(upstream:short)%00%(upstream:track,nobracket)%00%(HEAD)%00%(objectname)";

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Commit {
    pub sha: String,
    pub parents: Vec<String>,
    pub author: String,
    pub email: String,
    pub when: i64,
    pub subject: String,
    pub refs: Vec<Ref>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Ref {
    pub name: String,
    pub kind: RefKind,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RefKind {
    Head,
    Local,
    Remote,
    Tag,
    Stash,
    Detached,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CommitDetail {
    pub commit: Commit,
    pub body: String,
    pub files: Vec<FileStat>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FileStat {
    pub path: String,
    pub from: Option<String>,
    pub mark: Mark,
    pub added: u32,
    pub removed: u32,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Branch {
    pub name: String,
    pub upstream: Option<String>,
    pub ahead: u32,
    pub behind: u32,
    pub current: bool,
    pub tip: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Stash {
    pub index: u32,
    pub message: String,
    pub when: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UndoStep {
    pub sha: String,
    pub action: String,
    pub when: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Operation {
    Merge { theirs: String },
    Rebase { onto: String, step: u32, of: u32 },
    CherryPick { sha: String },
    Revert { sha: String },
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PullHow {
    Merge,
    Rebase,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Side {
    Ours,
    Theirs,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum DiffOf {
    Worktree,
    Index,
    Commit(String),
}

fn unreadable(what: &'static str, text: &str) -> GitError {
    GitError::Parse {
        what,
        text: text.to_owned(),
    }
}

fn number<T: std::str::FromStr>(text: &str, what: &'static str) -> Result<T, GitError> {
    text.parse().map_err(|_| unreadable(what, text))
}

fn refs(decorations: &str) -> Vec<Ref> {
    let mut refs = Vec::new();
    for item in decorations.split(", ").filter(|item| !item.is_empty()) {
        let full = match item.strip_prefix("HEAD -> ") {
            Some(branch) => {
                refs.push(Ref {
                    name: "HEAD".to_owned(),
                    kind: RefKind::Head,
                });
                branch
            }
            None => item,
        };
        let (kind, name) = if full == "HEAD" {
            (RefKind::Detached, full)
        } else if let Some(tag) = full.strip_prefix("tag: refs/tags/") {
            (RefKind::Tag, tag)
        } else if let Some(local) = full.strip_prefix("refs/heads/") {
            (RefKind::Local, local)
        } else if let Some(remote) = full.strip_prefix("refs/remotes/") {
            (RefKind::Remote, remote)
        } else if full == "refs/stash" {
            (RefKind::Stash, "stash")
        } else {
            continue;
        };
        refs.push(Ref {
            name: name.to_owned(),
            kind,
        });
    }
    refs
}

fn commit(record: &str) -> Result<(Commit, &str), GitError> {
    let fields: Vec<&str> = record.splitn(8, '\0').collect();
    let [
        sha,
        parents,
        author,
        email,
        when,
        subject,
        decorations,
        rest @ ..,
    ] = fields.as_slice()
    else {
        return Err(unreadable("commit record", record));
    };
    let commit = Commit {
        sha: (*sha).to_owned(),
        parents: parents.split_whitespace().map(str::to_owned).collect(),
        author: (*author).to_owned(),
        email: (*email).to_owned(),
        when: number(when, "commit time")?,
        subject: (*subject).to_owned(),
        refs: refs(decorations),
    };
    Ok((commit, rest.first().copied().unwrap_or("")))
}

fn commits(out: &str) -> Result<Vec<Commit>, GitError> {
    out.lines()
        .filter(|line| !line.is_empty())
        .map(|line| commit(line).map(|(commit, _)| commit))
        .collect()
}

fn name_status(out: &str) -> Result<Vec<(Mark, String, Option<String>)>, GitError> {
    let mut tokens = out
        .split('\0')
        .map(|token| token.trim_start_matches('\n'))
        .filter(|token| !token.is_empty());
    let mut files = Vec::new();
    while let Some(status) = tokens.next() {
        let code = status
            .chars()
            .next()
            .ok_or_else(|| unreadable("name status", status))?;
        let mut path = || {
            tokens
                .next()
                .map(str::to_owned)
                .ok_or_else(|| unreadable("name status without a path", status))
        };
        let file = match code {
            'R' | 'C' => {
                let from = path()?;
                (parse::mark(code, status)?, path()?, Some(from))
            }
            _ => (parse::mark(code, status)?, path()?, None),
        };
        files.push(file);
    }
    Ok(files)
}

fn numstat(out: &str) -> Result<Vec<(u32, u32)>, GitError> {
    let mut tokens = out
        .split('\0')
        .map(|token| token.trim_start_matches('\n'))
        .filter(|token| !token.is_empty());
    let mut counts = Vec::new();
    while let Some(record) = tokens.next() {
        let mut parts = record.splitn(3, '\t');
        let (Some(added), Some(removed), Some(path)) = (parts.next(), parts.next(), parts.next())
        else {
            return Err(unreadable("numstat record", record));
        };
        if path.is_empty() {
            tokens.next();
            tokens.next();
        }
        let count = |text: &str| match text {
            "-" => Ok(0),
            _ => number(text, "numstat count"),
        };
        counts.push((count(added)?, count(removed)?));
    }
    Ok(counts)
}

fn track(text: &str) -> Result<(u32, u32), GitError> {
    let (mut ahead, mut behind) = (0, 0);
    for part in text.split(", ").filter(|part| !part.is_empty()) {
        if let Some(count) = part.strip_prefix("ahead ") {
            ahead = number(count, "ahead count")?;
        } else if let Some(count) = part.strip_prefix("behind ") {
            behind = number(count, "behind count")?;
        } else if part != "gone" {
            return Err(unreadable("upstream track", text));
        }
    }
    Ok((ahead, behind))
}

fn braced(selector: &str) -> Option<&str> {
    selector.split_once("@{")?.1.strip_suffix('}')
}

fn action(subject: &str) -> String {
    let (verb, rest) = subject.split_once(": ").unwrap_or((subject, ""));
    match verb.split(' ').next().unwrap_or(verb) {
        "commit" if verb.contains("amend") => format!("Amend {rest}"),
        "commit" => format!("Commit {rest}"),
        "checkout" => rest
            .rsplit_once(" to ")
            .map_or_else(|| subject.to_owned(), |(_, to)| format!("Switch to {to}")),
        "pull" => format!("Pull {rest}"),
        "reset" => format!("Reset {}", rest.strip_prefix("moving ").unwrap_or(rest)),
        "merge" => format!("Merge {}", verb.trim_start_matches("merge ")),
        "rebase" => format!("Rebase {rest}"),
        "cherry-pick" => format!("Cherry-pick {rest}"),
        "revert" => format!("Revert {rest}"),
        _ => subject.to_owned(),
    }
    .trim_end()
    .to_owned()
}

fn read(dir: &Path, name: &str) -> Result<Option<String>, GitError> {
    let path = dir.join(name);
    match fs::read_to_string(&path) {
        Ok(text) => Ok(Some(text.trim().to_owned())),
        Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(None),
        Err(error) => Err(GitError::Read { path, error }),
    }
}

fn rebase(dir: &Path, step: &str, of: &str) -> Result<Option<Operation>, GitError> {
    let (Some(step), Some(of)) = (read(dir, step)?, read(dir, of)?) else {
        return Ok(None);
    };
    Ok(Some(Operation::Rebase {
        onto: read(dir, "onto")?.unwrap_or_default(),
        step: number(&step, "rebase step")?,
        of: number(&of, "rebase step count")?,
    }))
}

impl GitBinary {
    pub fn log(&self, skip: usize, count: usize) -> Result<Vec<Commit>, GitError> {
        let (skip, count) = (format!("--skip={skip}"), format!("--max-count={count}"));
        commits(&self.run(
            &[
                "log",
                "--topo-order",
                "--decorate=full",
                COMMIT,
                &skip,
                &count,
                "HEAD",
                "--branches",
                "--remotes",
                "--tags",
            ],
            None,
        )?)
    }

    pub fn show(&self, sha: &str) -> Result<CommitDetail, GitError> {
        let header = self.run(&["show", "-s", "--decorate=full", DETAIL, sha, "--"], None)?;
        let (commit, body) = commit(&header)?;
        let body = body.trim_end().to_owned();
        let files = |how: &str| {
            self.run(
                &[
                    "show",
                    "--format=",
                    "-z",
                    "-M",
                    "--diff-merges=first-parent",
                    how,
                    sha,
                    "--",
                ],
                None,
            )
        };
        let names = name_status(&files("--name-status")?)?;
        let counts = numstat(&files("--numstat")?)?;
        if names.len() != counts.len() {
            return Err(unreadable("file list whose stats do not match", sha));
        }
        let files = names
            .into_iter()
            .zip(counts)
            .map(|((mark, path, from), (added, removed))| FileStat {
                path,
                from,
                mark,
                added,
                removed,
            })
            .collect();
        Ok(CommitDetail {
            commit,
            body,
            files,
        })
    }

    pub fn file_diff(&self, path: &str, of: &DiffOf) -> Result<String, GitError> {
        let mut args = match of {
            DiffOf::Worktree => vec!["diff"],
            DiffOf::Index => vec!["diff", "--cached"],
            DiffOf::Commit(sha) => vec!["show", "--format=", "--diff-merges=first-parent", sha],
        };
        args.extend(["--no-color", "--no-ext-diff", "--", path]);
        self.run(&args, None)
    }

    pub fn branches_full(&self) -> Result<Vec<Branch>, GitError> {
        let out = self.run(&["for-each-ref", BRANCH, "refs/heads"], None)?;
        out.lines()
            .filter(|line| !line.is_empty())
            .map(|line| {
                let fields: Vec<&str> = line.split('\0').collect();
                let [name, upstream, tracking, head, tip] = fields.as_slice() else {
                    return Err(unreadable("branch record", line));
                };
                let (ahead, behind) = track(tracking)?;
                Ok(Branch {
                    name: (*name).to_owned(),
                    upstream: Some((*upstream).to_owned()).filter(|up| !up.is_empty()),
                    ahead,
                    behind,
                    current: *head == "*",
                    tip: (*tip).to_owned(),
                })
            })
            .collect()
    }

    pub fn remotes(&self) -> Result<Vec<(String, String)>, GitError> {
        let out = self.run(&["remote", "-v"], None)?;
        Ok(out
            .lines()
            .filter_map(|line| line.strip_suffix(" (fetch)")?.split_once('\t'))
            .map(|(name, url)| (name.to_owned(), url.to_owned()))
            .collect())
    }

    pub fn tags(&self) -> Result<Vec<String>, GitError> {
        let out = self.run(
            &[
                "for-each-ref",
                "--sort=-creatordate",
                "--format=%(refname:short)",
                "refs/tags",
            ],
            None,
        )?;
        Ok(out.lines().map(str::to_owned).collect())
    }

    pub fn stashes(&self) -> Result<Vec<Stash>, GitError> {
        let out = self.run(&["stash", "list", "--format=%gd%x00%ct%x00%gs"], None)?;
        out.lines()
            .filter(|line| !line.is_empty())
            .map(|line| {
                let fields: Vec<&str> = line.splitn(3, '\0').collect();
                let [selector, when, message] = fields.as_slice() else {
                    return Err(unreadable("stash record", line));
                };
                let index = braced(selector).ok_or_else(|| unreadable("stash name", line))?;
                Ok(Stash {
                    index: number(index, "stash index")?,
                    message: (*message).to_owned(),
                    when: number(when, "stash time")?,
                })
            })
            .collect()
    }

    pub fn undo_steps(&self, count: usize) -> Result<Vec<UndoStep>, GitError> {
        let count = format!("--max-count={count}");
        let out = self.run(
            &["reflog", &count, "--date=unix", "--format=%H%x00%gd%x00%gs"],
            None,
        )?;
        out.lines()
            .filter(|line| !line.is_empty())
            .map(|line| {
                let fields: Vec<&str> = line.splitn(3, '\0').collect();
                let [sha, selector, subject] = fields.as_slice() else {
                    return Err(unreadable("reflog record", line));
                };
                let when = braced(selector).ok_or_else(|| unreadable("reflog time", line))?;
                Ok(UndoStep {
                    sha: (*sha).to_owned(),
                    action: action(subject),
                    when: number(when, "reflog time")?,
                })
            })
            .collect()
    }

    pub fn operation(&self) -> Result<Option<Operation>, GitError> {
        let dir = self.run(&["rev-parse", "--absolute-git-dir"], None)?;
        let dir = Path::new(dir.trim_end());
        if let Some(rebasing) = rebase(&dir.join("rebase-merge"), "msgnum", "end")? {
            return Ok(Some(rebasing));
        }
        if let Some(rebasing) = rebase(&dir.join("rebase-apply"), "next", "last")? {
            return Ok(Some(rebasing));
        }
        if let Some(sha) = read(dir, "MERGE_HEAD")? {
            let merged_branch = read(dir, "MERGE_MSG")?.and_then(|message| {
                let first = message.lines().next()?;
                let rest = first.strip_prefix("Merge branch '")?;
                Some(rest.get(..rest.find('\'')?)?.to_owned())
            });
            let theirs = merged_branch
                .or_else(|| {
                    self.run(&["name-rev", "--name-only", "MERGE_HEAD"], None)
                        .ok()
                        .map(|name| name.trim().to_owned())
                        .filter(|name| !name.is_empty() && name != "undefined")
                })
                .unwrap_or(sha);
            return Ok(Some(Operation::Merge { theirs }));
        }
        if let Some(sha) = read(dir, "CHERRY_PICK_HEAD")? {
            return Ok(Some(Operation::CherryPick { sha }));
        }
        Ok(read(dir, "REVERT_HEAD")?.map(|sha| Operation::Revert { sha }))
    }

    pub fn stage_patch(&self, patch: &str, reverse: bool) -> Result<(), GitError> {
        let mut args = vec!["apply", "--cached", "--whitespace=nowarn"];
        if reverse {
            args.push("--reverse");
        }
        args.push("-");
        self.run(&args, Some(patch)).map(drop)
    }

    pub fn discard_patch(&self, patch: &str) -> Result<(), GitError> {
        self.run(
            &["apply", "--reverse", "--whitespace=nowarn", "-"],
            Some(patch),
        )
        .map(drop)
    }

    pub fn commit_with(&self, message: &str, amend: bool) -> Result<(), GitError> {
        let mut args = vec!["commit", "-q", "-F", "-"];
        if amend {
            args.push("--amend");
        }
        self.run(&args, Some(message)).map(drop)
    }

    pub fn create_branch(&self, name: &str, at: &str) -> Result<(), GitError> {
        self.run(&["branch", "--", name, at], None).map(drop)
    }

    pub fn delete_branch(&self, name: &str) -> Result<(), GitError> {
        self.run(&["branch", "-d", "--", name], None).map(drop)
    }

    pub fn fetch(&self) -> Result<(), GitError> {
        self.run(&["fetch", "--all", "--prune", "-q"], None)
            .map(drop)
    }

    pub fn pull(&self, how: PullHow) -> Result<(), GitError> {
        let how = match how {
            PullHow::Merge => "--no-rebase",
            PullHow::Rebase => "--rebase",
        };
        self.run(&["pull", "-q", how], None).map(drop)
    }

    pub fn push(&self, lease: bool) -> Result<(), GitError> {
        let mut args = vec!["push", "-q"];
        if lease {
            args.push("--force-with-lease");
        }
        self.run(&args, None).map(drop)
    }

    pub fn outgoing(&self) -> Result<Vec<Commit>, GitError> {
        let upstream = self
            .branches_full()?
            .into_iter()
            .find(|branch| branch.current)
            .and_then(|branch| branch.upstream);
        let mut args = vec!["log", "--topo-order", "--decorate=full", COMMIT];
        let range = upstream.map(|upstream| format!("{upstream}..HEAD"));
        match &range {
            Some(range) => args.push(range),
            None => args.extend(["HEAD", "--not", "--remotes"]),
        }
        commits(&self.run(&args, None)?)
    }

    pub fn stash_push(&self, message: &str) -> Result<(), GitError> {
        self.run(
            &["stash", "push", "-q", "--include-untracked", "-m", message],
            None,
        )
        .map(drop)
    }

    pub fn stash_pop(&self, index: u32) -> Result<(), GitError> {
        let selector = format!("stash@{{{index}}}");
        self.run(&["stash", "pop", "-q", &selector], None).map(drop)
    }

    pub fn stash_drop(&self, index: u32) -> Result<(), GitError> {
        let selector = format!("stash@{{{index}}}");
        self.run(&["stash", "drop", "-q", &selector], None)
            .map(drop)
    }

    pub fn restore_to(&self, sha: &str, keep_changes: bool) -> Result<(), GitError> {
        if keep_changes {
            return self.run(&["reset", "-q", "--mixed", sha], None).map(drop);
        }
        let backup = self.run(&["stash", "create"], None)?;
        let backup = backup.trim();
        if !backup.is_empty() {
            self.run(
                &["stash", "store", "-q", "-m", "desk undo backup", backup],
                None,
            )?;
        }
        self.run(&["reset", "-q", "--hard", sha], None).map(drop)
    }

    pub fn take_side(&self, path: &str, side: Side) -> Result<(), GitError> {
        let side = match side {
            Side::Ours => "--ours",
            Side::Theirs => "--theirs",
        };
        self.run(&["checkout", side, "--", path], None)?;
        self.mark_resolved(path)
    }

    pub fn mark_resolved(&self, path: &str) -> Result<(), GitError> {
        self.run(&["add", "--", path], None).map(drop)
    }

    pub fn continue_operation(&self) -> Result<(), GitError> {
        let args: &[&str] = match self.operation()?.ok_or(GitError::Idle)? {
            Operation::Merge { .. } => &["commit", "--no-edit"],
            Operation::Rebase { .. } => &["rebase", "--continue"],
            Operation::CherryPick { .. } => &["cherry-pick", "--continue"],
            Operation::Revert { .. } => &["revert", "--continue"],
        };
        self.run(args, None).map(drop)
    }

    pub fn abort_operation(&self) -> Result<(), GitError> {
        let args: &[&str] = match self.operation()?.ok_or(GitError::Idle)? {
            Operation::Merge { .. } => &["merge", "--abort"],
            Operation::Rebase { .. } => &["rebase", "--abort"],
            Operation::CherryPick { .. } => &["cherry-pick", "--abort"],
            Operation::Revert { .. } => &["revert", "--abort"],
        };
        self.run(args, None).map(drop)
    }
}
