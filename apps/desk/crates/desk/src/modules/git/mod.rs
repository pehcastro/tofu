mod changes;
mod fixture;
mod history;
mod kit;
mod watch;

use std::collections::{BTreeSet, HashMap};
use std::hash::{DefaultHasher, Hash, Hasher};
use std::path::Path;
use std::time::Instant;

use desk_core::git::{Entry, Git as _, GitBinary, GitError, Mark, State};
use desk_ui::components::chip::GitStatus;
use desk_ui::components::file_edits::{ChangeAction, ChangedFile};
use desk_ui::components::form::TextArea;
use desk_ui::live::ActiveTheme;
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Entity, Render, SharedString, Task, Window,
    prelude::*, px,
};

use fixture::{CHANGES, COMMITTED};

const GAP: f32 = 6.0;
const MESSAGE_HINT: &str = "Commit message";
const DETACHED: &str = "detached HEAD";

pub fn open(board: Option<&str>, window: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    kit::load_fonts(cx)?;
    let (pane, branches, blocked) = match board {
        None | Some("IGIT-1") => (Pane::Changes, false, false),
        Some("IGIT-2") => (Pane::History, false, false),
        Some("S-GIT-1") => (Pane::History, true, false),
        Some("S-GIT-2") => (Pane::History, false, true),
        Some(other) => {
            return Err(format!(
                "the git module draws IGIT-1, IGIT-2, S-GIT-1 and S-GIT-2, not {other}"
            ));
        }
    };
    let changes = Changes::Fixture([false; CHANGES.len()]);
    Ok(Git::open(pane, changes, branches, blocked, window, cx).into())
}

pub fn open_repo(
    path: &Path,
    board: Option<&str>,
    window: &mut Window,
    cx: &mut App,
) -> Result<AnyView, String> {
    if let Some(other) = board.filter(|board| *board != "IGIT-1") {
        return Err(format!(
            "--repo draws the Changes pane, IGIT-1, not {other}"
        ));
    }
    Ok(mount(path, window, cx)?.into())
}

pub fn mount(project: &Path, window: &mut Window, cx: &mut App) -> Result<Entity<Git>, String> {
    kit::load_fonts(cx)?;
    let changes = match Repo::open(project) {
        Ok(repo) => {
            eprintln!(
                "desk: git opened {} on {}: {} changes",
                repo.git.root().display(),
                repo.branch,
                repo.files.len()
            );
            Changes::Repo(Box::new(repo))
        }
        Err(error) => {
            eprintln!("desk: git could not open {}: {error}", project.display());
            Changes::Refused(error.to_string().into())
        }
    };
    let git = Git::open(Pane::Changes, changes, false, false, window, cx);
    git.update(cx, |git, cx| git.watch(cx));
    Ok(git)
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Pane {
    Changes,
    History,
}

enum Changes {
    Fixture([bool; CHANGES.len()]),
    Repo(Box<Repo>),
    Refused(SharedString),
}

struct Repo {
    git: GitBinary,
    branch: SharedString,
    files: Vec<ChangedFile>,
    first_seen: HashMap<SharedString, Option<u64>>,
    ignored: BTreeSet<String>,
    skips: Option<flume::Sender<BTreeSet<String>>>,
}

impl Repo {
    fn open(path: &Path) -> Result<Repo, GitError> {
        let mut repo = Repo {
            git: GitBinary::open(path)?,
            branch: SharedString::default(),
            files: Vec::new(),
            first_seen: HashMap::new(),
            ignored: BTreeSet::new(),
            skips: None,
        };
        repo.reload()?;
        repo.reignore()?;
        Ok(repo)
    }

    fn reignore(&mut self) -> Result<(), GitError> {
        let ignored = self.git.ignored()?;
        if ignored != self.ignored {
            if let Some(skips) = &self.skips
                && skips.send(ignored.clone()).is_err()
            {
                eprintln!("desk: git watcher is gone, so ignored paths are not skipped");
            }
            self.ignored = ignored;
        }
        Ok(())
    }

    fn reload(&mut self) -> Result<(), GitError> {
        self.branch = self
            .git
            .current_branch()?
            .unwrap_or_else(|| DETACHED.to_owned())
            .into();
        self.files = self.git.changes()?.into_iter().flat_map(rows).collect();
        let mut was = std::mem::take(&mut self.first_seen);
        self.first_seen = self
            .files
            .iter()
            .filter(|file| !file.staged)
            .map(|file| {
                let seen = was
                    .remove(&file.path)
                    .unwrap_or_else(|| self.fingerprint(&file.path));
                (file.path.clone(), seen)
            })
            .collect();
        Ok(())
    }

    fn fingerprint(&self, path: &str) -> Option<u64> {
        let bytes = std::fs::read(self.git.root().join(path)).ok()?;
        let mut hasher = DefaultHasher::new();
        bytes.hash(&mut hasher);
        Some(hasher.finish())
    }

    fn moved_since_edit(&self, path: &SharedString) -> bool {
        self.first_seen
            .get(path)
            .is_some_and(|seen| *seen != self.fingerprint(path))
    }

    fn undo(&mut self, path: &str) -> Option<SharedString> {
        let started = Instant::now();
        if let Err(error) = self.git.discard(&[path.to_owned()]) {
            return logged(&format!("undo {path}"), Err(error));
        }
        let restored = started.elapsed().as_millis();
        let reloading = Instant::now();
        let done = self.reload();
        eprintln!(
            "desk: undo restored {path} in {restored} ms, list reloaded in {} ms",
            reloading.elapsed().as_millis()
        );
        logged(&format!("undo {path}"), done)
    }

    fn run(&mut self, action: ChangeAction, path: &str) -> Result<(), GitError> {
        let paths = [path.to_owned()];
        match action {
            ChangeAction::Stage => self.git.stage(&paths),
            ChangeAction::Unstage => self.git.unstage(&paths),
            ChangeAction::Discard => self.git.discard(&paths),
        }?;
        self.reload()
    }
}

fn shown(mark: Mark) -> Option<GitStatus> {
    match mark {
        Mark::Unmodified => None,
        Mark::Modified | Mark::TypeChanged => Some(GitStatus::Modified),
        Mark::Added | Mark::Copied | Mark::Renamed => Some(GitStatus::Added),
        Mark::Deleted => Some(GitStatus::Deleted),
    }
}

fn rows(entry: Entry) -> impl Iterator<Item = ChangedFile> {
    let (index, worktree) = match entry.state {
        State::Tracked { index, worktree }
        | State::Renamed {
            index, worktree, ..
        } => (shown(index), shown(worktree)),
        State::Conflicted(_) => (None, Some(GitStatus::Conflict)),
        State::Untracked => (None, Some(GitStatus::Untracked)),
        State::Ignored => (None, None),
    };
    let path = SharedString::from(entry.path);
    [(index, true), (worktree, false)]
        .into_iter()
        .filter_map(move |(status, staged)| {
            Some(ChangedFile {
                path: path.clone(),
                status: status?,
                staged,
            })
        })
}

fn logged(what: &str, done: Result<(), GitError>) -> Option<SharedString> {
    match done {
        Ok(()) => {
            eprintln!("desk: git {what} ok");
            None
        }
        Err(error) => {
            eprintln!("desk: git {what}: {error}");
            Some(error.to_string().into())
        }
    }
}

pub struct Git {
    pane: Pane,
    changes: Changes,
    message: Entity<TextArea>,
    commit: usize,
    branches: bool,
    blocked: bool,
    told: Option<SharedString>,
    asking: Option<SharedString>,
    _watching: Option<Task<()>>,
}

impl Git {
    fn open(
        pane: Pane,
        changes: Changes,
        branches: bool,
        blocked: bool,
        window: &mut Window,
        cx: &mut App,
    ) -> Entity<Self> {
        cx.new(|cx| Git {
            pane,
            changes,
            message: cx.new(|cx| TextArea::new(MESSAGE_HINT.into(), window, cx)),
            commit: 0,
            branches,
            blocked,
            told: None,
            asking: None,
            _watching: None,
        })
    }

    fn watch(&mut self, cx: &mut Context<Self>) {
        let Changes::Repo(repo) = &mut self.changes else {
            return;
        };
        let (skips, changes) = match watch::watch(repo.git.root().to_owned(), repo.ignored.clone())
        {
            Ok(found) => found,
            Err(error) => return eprintln!("desk: git does not watch the tree: {error}"),
        };
        repo.skips = Some(skips);
        self._watching = Some(cx.spawn(async move |this, cx| {
            while let Ok(first) = changes.recv_async().await {
                let reshaped = changes.drain().fold(first, |any, next| any || next);
                if this
                    .update(cx, |git, cx| git.refresh(reshaped, cx))
                    .is_err()
                {
                    return;
                }
            }
        }));
    }

    fn refresh(&mut self, reshaped: bool, cx: &mut Context<Self>) {
        let Changes::Repo(repo) = &mut self.changes else {
            return;
        };
        let started = Instant::now();
        let reignored = match reshaped {
            true => repo.reignore(),
            false => Ok(()),
        };
        match reignored.and_then(|()| repo.reload()) {
            Ok(()) => eprintln!(
                "desk: git tree changed, {} changes in {} ms",
                repo.files.len(),
                started.elapsed().as_millis()
            ),
            Err(error) => {
                eprintln!("desk: git reload: {error}");
                self.told = Some(error.to_string().into());
            }
        }
        cx.notify();
    }

    fn files(&self) -> Vec<ChangedFile> {
        match &self.changes {
            Changes::Fixture(staged) => CHANGES
                .iter()
                .zip(staged)
                .map(|(change, &staged)| ChangedFile {
                    path: change.path.into(),
                    status: if change.tracked {
                        GitStatus::Modified
                    } else {
                        GitStatus::Untracked
                    },
                    staged,
                })
                .collect(),
            Changes::Repo(repo) => repo.files.clone(),
            Changes::Refused(_) => Vec::new(),
        }
    }

    fn act(&mut self, ix: usize, action: ChangeAction, cx: &mut Context<Self>) {
        self.told = match &mut self.changes {
            Changes::Fixture(staged) => match (action, CHANGES.get(ix), staged.get_mut(ix)) {
                (ChangeAction::Discard, _, _) => {
                    Some("Undo needs a repository: open the desk with --repo.".into())
                }
                (_, Some(change), Some(on)) if change.stageable => {
                    *on = !*on;
                    None
                }
                (_, Some(change), _) => Some(change.tell.into()),
                (_, None, _) => None,
            },
            Changes::Repo(repo) => {
                let Some(path) = repo.files.get(ix).map(|file| file.path.clone()) else {
                    return;
                };
                match action {
                    ChangeAction::Discard if repo.moved_since_edit(&path) => {
                        let told = format!("{path} changed on disk after the edit.");
                        self.asking = Some(path);
                        Some(told.into())
                    }
                    ChangeAction::Discard => repo.undo(&path),
                    ChangeAction::Stage | ChangeAction::Unstage => logged(
                        &format!("{} {path}", action.verb()),
                        repo.run(action, &path),
                    ),
                }
            }
            Changes::Refused(_) => None,
        };
        cx.notify();
    }

    fn answer(&mut self, overwrite: bool, cx: &mut Context<Self>) {
        let path = self.asking.take();
        self.told = match (&mut self.changes, path) {
            (Changes::Repo(repo), Some(path)) if overwrite => repo.undo(&path),
            _ => {
                eprintln!("desk: undo cancelled");
                None
            }
        };
        cx.notify();
    }

    fn commit(&mut self, cx: &mut Context<Self>) {
        let message = self.message.read(cx).text();
        let staged = self.files().iter().filter(|file| file.staged).count();
        let what = format!("commit {message}");
        let (committed, told) = match &mut self.changes {
            _ if message.trim().is_empty() => {
                eprintln!("desk: git commit refused: the message is empty");
                (false, Some("Write a commit message first.".into()))
            }
            _ if staged == 0 => {
                eprintln!("desk: git commit refused: nothing is staged");
                (false, Some("Stage a file first.".into()))
            }
            Changes::Fixture(staged) => {
                *staged = [false; CHANGES.len()];
                (
                    true,
                    Some(format!("Committed {COMMITTED} \u{b7} {message}. tofu never commits on its own; this was you.").into()),
                )
            }
            Changes::Repo(repo) => {
                let done = repo.git.commit(&message).and_then(|()| repo.reload());
                let told = logged(&what, done);
                (told.is_none(), told)
            }
            Changes::Refused(_) => (false, None),
        };
        if committed {
            self.message.update(cx, |area, cx| area.clear(cx));
        }
        self.told = told;
        cx.notify();
    }

    fn tell(
        message: &'static str,
    ) -> impl Fn(&mut Self, &ClickEvent, &mut Window, &mut Context<Self>) {
        move |this, _, _, cx| {
            this.told = Some(message.into());
            cx.notify();
        }
    }
}

impl Render for Git {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let body = match self.pane {
            Pane::Changes => self.changes(&theme, cx),
            Pane::History => self.history(&theme, cx),
        };
        kit::frame(
            &theme,
            body.gap(px(GAP)),
            self.told.clone(),
            cx.listener(|this, _: &ClickEvent, _, cx| {
                this.told = None;
                cx.notify();
            }),
        )
    }
}
