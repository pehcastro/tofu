use std::sync::Arc;
use std::sync::atomic::Ordering;
use std::time::Instant;

use desk_core::git::{DiffOf, Git, GitBinary, GitError, PullHow, Side};
use gpui::{AppContext, Context};

use super::model::{Action, GitScreen, Loaded, Snapshot, Told};
use super::sync::{plural, short};
use super::watch::{self, Touched};

pub const UNDO_STEPS: usize = 20;

pub fn loaded<T>(read: Result<T, GitError>) -> Loaded<T> {
    match read {
        Ok(value) => Loaded::Ready(value),
        Err(error) => Loaded::Failed(error.to_string()),
    }
}

fn read_snapshot(git: &GitBinary) -> Result<Snapshot, GitError> {
    let branches = git.branches_full()?;
    Ok(Snapshot {
        branch: branches.iter().find(|branch| branch.current).cloned(),
        branches,
        remotes: git.remotes()?,
        tags: git.tags()?,
        stashes: git.stashes()?,
        changes: git.changes()?,
        operation: git.operation()?,
    })
}

struct Words {
    branch: String,
    upstream: String,
    behind: u32,
}

impl Words {
    fn of(snapshot: &Snapshot) -> Words {
        let branch = snapshot.branch.as_ref();
        Words {
            branch: branch.map_or_else(|| "HEAD".to_owned(), |branch| branch.name.clone()),
            upstream: branch
                .and_then(|branch| branch.upstream.clone())
                .unwrap_or_else(|| "its remote".to_owned()),
            behind: branch.map_or(0, |branch| branch.behind),
        }
    }

    fn remote(&self) -> &str {
        self.upstream
            .split_once('/')
            .map_or(self.upstream.as_str(), |(remote, _)| remote)
    }
}

fn perform(git: &GitBinary, action: &Action, words: &Words) -> Result<String, GitError> {
    let (branch, upstream) = (&words.branch, &words.upstream);
    Ok(match action {
        Action::Stage(paths) => {
            git.stage(paths)?;
            format!("Staged {}", plural(paths.len(), "file"))
        }
        Action::Unstage(paths) => {
            git.unstage(paths)?;
            format!("Unstaged {}", plural(paths.len(), "file"))
        }
        Action::Discard(paths) => {
            git.discard(paths)?;
            format!("Discarded the changes in {}", plural(paths.len(), "file"))
        }
        Action::StagePatch(patch) => {
            git.stage_patch(patch, false)?;
            "Staged the hunk".to_owned()
        }
        Action::DiscardPatch(patch) => {
            git.discard_patch(patch)?;
            "Discarded the hunk".to_owned()
        }
        Action::Commit {
            message,
            amend,
            push,
        } => {
            git.commit_with(message, *amend)?;
            let subject = message.lines().next().unwrap_or_default();
            let done = match amend {
                true => format!("Amended the last commit: {subject}"),
                false => format!("Committed {subject}"),
            };
            if !push {
                return Ok(done);
            }
            git.push(false)?;
            format!("{done}, pushed {branch} to {upstream}")
        }
        Action::Switch(name) => {
            git.switch(name)?;
            format!("Switched to {name}")
        }
        Action::CreateBranch { name, at } => {
            git.create_branch(name, at)?;
            format!("Created {name} at {}", short(at))
        }
        Action::Fetch => {
            git.fetch()?;
            "Fetched from every remote".to_owned()
        }
        Action::Pull(how) => {
            git.pull(*how)?;
            let how = match how {
                PullHow::Merge => "merged",
                PullHow::Rebase => "rebased",
            };
            format!("Pulled {upstream} into {branch}, {how}")
        }
        Action::Push { lease } => {
            git.push(*lease)?;
            format!("Pushed {branch} to {upstream}")
        }
        Action::StashPush(message) => {
            git.stash_push(message)?;
            "Stashed your changes".to_owned()
        }
        Action::StashPop(index) => {
            git.stash_pop(*index)?;
            format!("Applied stash@{{{index}}} and dropped it")
        }
        Action::StashDrop(index) => {
            git.stash_drop(*index)?;
            format!("Dropped stash@{{{index}}}")
        }
        Action::Restore { sha, keep } => {
            git.restore_to(sha, *keep)?;
            format!("Restored to {}", short(sha))
        }
        Action::Take { path, side } => {
            git.take_side(path, *side)?;
            let kept = match side {
                Side::Ours => "ours",
                Side::Theirs => "theirs",
            };
            format!("Kept {kept} for {path}")
        }
        Action::Resolve(path) => {
            git.mark_resolved(path)?;
            format!("Marked {path} resolved")
        }
        Action::Continue => {
            git.continue_operation()?;
            "Continued".to_owned()
        }
        Action::Abort => {
            git.abort_operation()?;
            "Aborted".to_owned()
        }
    })
}

fn verb(action: &Action) -> &'static str {
    match action {
        Action::Stage(_) | Action::StagePatch(_) => "Stage",
        Action::Unstage(_) => "Unstage",
        Action::Discard(_) | Action::DiscardPatch(_) => "Discard",
        Action::Commit { .. } => "Commit",
        Action::Switch(_) => "Switch",
        Action::CreateBranch { .. } => "Branch",
        Action::Fetch => "Fetch",
        Action::Pull(_) => "Pull",
        Action::Push { .. } => "Push",
        Action::StashPush(_) | Action::StashPop(_) | Action::StashDrop(_) => "Stash",
        Action::Restore { .. } => "Restore",
        Action::Take { .. } | Action::Resolve(_) => "Resolve",
        Action::Continue => "Continue",
        Action::Abort => "Abort",
    }
}

fn failed(action: &Action, error: &GitError, words: &Words) -> Told {
    let stderr = match error {
        GitError::Failed { stderr, .. } => Some(stderr.as_str()),
        GitError::Spawn(_) | GitError::Parse { .. } | GitError::Read { .. } | GitError::Idle => {
            None
        }
    };
    let refused =
        stderr.is_some_and(|said| said.contains("rejected") || said.contains("fetch first"));
    if matches!(
        action,
        Action::Push { .. } | Action::Commit { push: true, .. }
    ) && refused
    {
        let has = match words.behind {
            0 => "commits".to_owned(),
            1 => "1 commit".to_owned(),
            count => format!("{count} commits"),
        };
        return Told::Failed {
            said: format!(
                "Push refused: {} has {has} you do not have. Pull first.",
                words.remote()
            ),
            fix: Some(Action::Pull(PullHow::Merge)),
        };
    }
    let why = stderr
        .and_then(|said| said.lines().map(str::trim).find(|line| !line.is_empty()))
        .map_or_else(|| error.to_string(), str::to_owned);
    Told::Failed {
        said: format!("{} failed: {why}", verb(action)),
        fix: None,
    }
}

impl GitScreen {
    pub(super) fn fetch<T: Send + 'static>(
        &self,
        read: impl FnOnce(&GitBinary) -> Result<T, GitError> + Send + 'static,
        keep: impl FnOnce(&mut GitScreen, Loaded<T>, &mut Context<GitScreen>) + 'static,
        cx: &mut Context<Self>,
    ) {
        let Some(git) = self.binary() else {
            return;
        };
        let read = cx.background_spawn(async move { read(&git) });
        cx.spawn(async move |screen, cx| {
            let read = loaded(read.await);
            let kept = screen.update(cx, |screen, cx| {
                keep(screen, read, cx);
                cx.notify();
            });
            if let Err(error) = kept {
                eprintln!("desk: git: an answer came back to a closed screen: {error}");
            }
        })
        .detach();
    }

    pub(super) fn open_repo(&mut self, cx: &mut Context<Self>) {
        let root = self.root.clone();
        let opened = cx.background_spawn(async move { GitBinary::open(&root) });
        cx.spawn(async move |screen, cx| {
            let opened = opened.await;
            if let Err(error) = screen.update(cx, |screen, cx| screen.opened_repo(opened, cx)) {
                eprintln!("desk: git: the repository opened after the screen closed: {error}");
            }
        })
        .detach();
    }

    fn opened_repo(&mut self, opened: Result<GitBinary, GitError>, cx: &mut Context<Self>) {
        let git = match opened {
            Ok(git) => Arc::new(git),
            Err(error) => {
                eprintln!(
                    "desk: git: {} is not a repository: {error}",
                    self.root.display()
                );
                self.git = Loaded::Failed(error.to_string());
                return cx.notify();
            }
        };
        self.git = Loaded::Ready(Arc::clone(&git));
        match watch::start(git, Arc::clone(&self.paused)) {
            Ok(touched) => {
                self._watching = Some(cx.spawn(async move |screen, cx| {
                    while let Ok(seen) = touched.recv_async().await {
                        if screen
                            .update(cx, |screen, cx| screen.reload(seen, cx))
                            .is_err()
                        {
                            return;
                        }
                    }
                }));
            }
            Err(error) => eprintln!("desk: git: {error}"),
        }
        self.reload(Touched::Git, cx);
    }

    pub(super) fn reload(&mut self, touched: Touched, cx: &mut Context<Self>) {
        if self.reloading {
            self.wanted = self.wanted.max(Some(touched));
            return;
        }
        self.reloading = true;
        if touched == Touched::Git {
            self.reload_history(cx);
        }
        let started = Instant::now();
        self.fetch(read_snapshot, move |screen, read, cx| {
            screen.reloading = false;
            match read {
                Loaded::Ready(snapshot) => {
                    if !screen.read {
                        eprintln!(
                            "desk: git: snapshot of {} changed files read {} ms after open, {} ms to read",
                            snapshot.changes.len(),
                            screen.opened.elapsed().as_millis(),
                            started.elapsed().as_millis()
                        );
                    }
                    screen.snapshot = snapshot;
                    screen.read = true;
                    screen.problem = None;
                }
                Loaded::Failed(error) => {
                    eprintln!("desk: git: the snapshot was not read: {error}");
                    screen.problem = Some(error);
                }
                Loaded::Waiting => {}
            }
            if let Some(next) = screen.wanted.take() {
                screen.reload(next, cx);
            }
        }, cx);
    }

    pub fn run(&mut self, action: Action, cx: &mut Context<Self>) {
        if self.running {
            return eprintln!("desk: git: {} waits, a command is running", verb(&action));
        }
        let Some(git) = self.binary() else {
            return eprintln!("desk: git: {} has no repository", verb(&action));
        };
        let words = Words::of(&self.snapshot);
        self.running = true;
        self.paused.store(true, Ordering::Release);
        let done = cx.background_spawn(async move {
            perform(&git, &action, &words)
                .map(Told::Done)
                .unwrap_or_else(|error| failed(&action, &error, &words))
        });
        cx.spawn(async move |screen, cx| {
            let told = done.await;
            let kept = screen.update(cx, |screen, cx| {
                screen.running = false;
                screen.paused.store(false, Ordering::Release);
                screen.tell(told, cx);
                screen.reload(Touched::Git, cx);
            });
            if let Err(error) = kept {
                eprintln!("desk: git: a command finished after the screen closed: {error}");
            }
        })
        .detach();
        cx.notify();
    }

    pub(super) fn load_detail(&mut self, sha: String, cx: &mut Context<Self>) {
        if matches!(&self.detail, Some((loaded, Loaded::Ready(_) | Loaded::Waiting)) if *loaded == sha)
        {
            return;
        }
        self.detail = Some((sha.clone(), Loaded::Waiting));
        let asked = sha.clone();
        self.fetch(
            move |git| git.show(&asked),
            move |screen, read, _| {
                if matches!(&screen.detail, Some((loaded, _)) if *loaded == sha) {
                    screen.detail = Some((sha, read));
                }
            },
            cx,
        );
    }

    pub(super) fn load_diff(&mut self, path: String, of: DiffOf, cx: &mut Context<Self>) {
        self.diff = Some((path.clone(), Loaded::Waiting));
        let asked = path.clone();
        self.fetch(
            move |git| git.file_diff(&asked, &of),
            move |screen, read, _| {
                if matches!(&screen.diff, Some((loaded, _)) if *loaded == path) {
                    screen.diff = Some((path, read));
                }
            },
            cx,
        );
    }

    pub fn want_undo(&mut self, cx: &mut Context<Self>) {
        self.undo = Loaded::Waiting;
        self.fetch(
            |git| git.undo_steps(UNDO_STEPS),
            |screen, read, _| screen.undo = read,
            cx,
        );
    }

    pub fn want_outgoing(&mut self, cx: &mut Context<Self>) {
        self.outgoing = Loaded::Waiting;
        self.fetch(
            GitBinary::outgoing,
            |screen, read, _| screen.outgoing = read,
            cx,
        );
    }
}
