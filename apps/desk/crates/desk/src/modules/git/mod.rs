mod changes;
mod fixture;
mod history;
mod kit;

use std::path::Path;

use desk_core::git::{Entry, Git as _, GitBinary, GitError, Mark, State};
use desk_ui::components::chip::GitStatus;
use desk_ui::components::file_edits::{ChangeAction, ChangedFile};
use desk_ui::components::form::TextArea;
use desk_ui::live::ActiveTheme;
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Entity, Render, SharedString, Window,
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
    kit::load_fonts(cx)?;
    let changes = match Repo::open(path) {
        Ok(repo) => {
            eprintln!(
                "desk: git opened {} on {}: {} changes",
                repo.git.root().display(),
                repo.branch,
                repo.files.len()
            );
            Changes::Repo(repo)
        }
        Err(error) => {
            eprintln!("desk: git could not open {}: {error}", path.display());
            Changes::Refused(error.to_string().into())
        }
    };
    Ok(Git::open(Pane::Changes, changes, false, false, window, cx).into())
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Pane {
    Changes,
    History,
}

enum Changes {
    Fixture([bool; CHANGES.len()]),
    Repo(Repo),
    Refused(SharedString),
}

struct Repo {
    git: GitBinary,
    branch: SharedString,
    files: Vec<ChangedFile>,
}

impl Repo {
    fn open(path: &Path) -> Result<Repo, GitError> {
        let mut repo = Repo {
            git: GitBinary::open(path)?,
            branch: SharedString::default(),
            files: Vec::new(),
        };
        repo.reload()?;
        Ok(repo)
    }

    fn reload(&mut self) -> Result<(), GitError> {
        self.branch = self
            .git
            .current_branch()?
            .unwrap_or_else(|| DETACHED.to_owned())
            .into();
        self.files = self.git.status()?.into_iter().flat_map(rows).collect();
        Ok(())
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
        })
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
                    Some("Discarding needs a repository: open the desk with --repo.".into())
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
                logged(
                    &format!("{} {path}", action.verb()),
                    repo.run(action, &path),
                )
            }
            Changes::Refused(_) => None,
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
