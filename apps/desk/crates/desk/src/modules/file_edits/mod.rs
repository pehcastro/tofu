use super::chat::{Trace, mention};
use super::replayed::{self, Replayed};

use std::rc::Rc;

use desk_core::model::{Session, Store};
use desk_core::protocol::{FileEdit as Edit, FileEditOp, HunkLineKind};
use desk_ui::components::chip;
use desk_ui::components::diff::{FileChange, FileDiff};
use desk_ui::components::file_edits::{EditedFile, FileEdit, file_edits, file_history};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::sheet::Drawer;
use desk_ui::live::ActiveTheme;
use gpui::{
    AnyView, App, AppContext, Context, Entity, IntoElement, Render, SharedString, Subscription,
    Window, div, prelude::*,
};

const BOARD: &str = "36-agents";
const TILE_WIDTH: f32 = 513.0;

pub struct FileEdits {
    store: Entity<Store>,
    files: Vec<EditedFile>,
    traces: Vec<Vec<(SharedString, Trace)>>,
    opened: usize,
    drawer: bool,
    _watch: Subscription,
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if board.is_some_and(|board| board != BOARD) {
        return Err(format!(
            "the file edits module replays {BOARD}, not {board:?}"
        ));
    }
    replayed::fonts(cx)?;
    let store = cx.new(|_| Store::default());
    let module = mount(store.clone(), cx);
    let counted = module.clone();
    Replayed::open(
        store,
        module.into(),
        (Glyph::File, "File edits", TILE_WIDTH),
        move |cx| {
            let line = counted.update(cx, |module, cx| module.counted(cx));
            eprintln!(
                "desk: file edits from the store: {line}; cassette: {} file.edit events",
                cassette_edits()
            );
        },
        cx,
    )
}

pub fn mount(store: Entity<Store>, cx: &mut App) -> Entity<FileEdits> {
    cx.new(|cx: &mut Context<FileEdits>| FileEdits {
        _watch: cx.observe(&store, |module, _, cx| {
            module.rebuild(cx);
            eprintln!("desk: tile file edits rebuilt {} files", module.files.len());
            cx.notify();
        }),
        store,
        files: Vec::new(),
        traces: Vec::new(),
        opened: 0,
        drawer: false,
    })
}

fn patch(edit: &Edit) -> String {
    let mut text = String::new();
    for hunk in &edit.hunks {
        let without = |skip: HunkLineKind| hunk.lines.iter().filter(|l| l.kind != skip).count();
        text += &format!(
            "@@ -{},{} +{},{} @@\n",
            hunk.old_start,
            without(HunkLineKind::Added),
            hunk.new_start,
            without(HunkLineKind::Removed)
        );
        for line in &hunk.lines {
            let sign = match line.kind {
                HunkLineKind::Added => '+',
                HunkLineKind::Removed => '-',
                HunkLineKind::Context | HunkLineKind::Unknown(_) => ' ',
            };
            text += &format!("{sign}{}\n", line.text);
        }
    }
    text
}

fn shown(session: &Session, edit: &Edit) -> Result<FileEdit, String> {
    let change = match &edit.op {
        FileEditOp::Create => FileChange::Added,
        FileEditOp::Modify => FileChange::Modified,
        FileEditOp::Unknown(op) => return Err(format!("{}: unknown op {op}", edit.path)),
    };
    let diff = FileDiff::parse(edit.path.clone(), change, &patch(edit), |_| Vec::new())
        .map_err(|error| format!("{}: {error:?}", edit.path))?;
    let agent = edit.agent.as_ref().map(|id| {
        session.agents.get(id).map_or_else(
            || SharedString::from(id.clone()),
            |agent| format!("{} {}", agent.kind, agent.number).into(),
        )
    });
    Ok(FileEdit {
        agent,
        at: shown_at(session, edit).into(),
        diff: Rc::new(diff),
    })
}

fn shown_at(session: &Session, edit: &Edit) -> String {
    session
        .tools
        .get(&edit.item)
        .and_then(|tool| tool.started_at.as_deref()?.get(11..19))
        .map_or_else(|| format!("seq {}", edit.seq), str::to_owned)
}

type EditMentions = Vec<(SharedString, Trace)>;

fn files(session: &Session) -> Vec<(EditedFile, EditMentions)> {
    let mut files: Vec<(i64, EditedFile, EditMentions)> = session
        .files
        .iter()
        .map(|(path, edits)| {
            let shown: Vec<FileEdit> = edits
                .iter()
                .filter_map(|edit| {
                    shown(session, edit)
                        .map_err(|error| eprintln!("desk: file edits: {error}"))
                        .ok()
                })
                .collect();
            let mut by: Vec<SharedString> = Vec::new();
            for agent in shown.iter().rev().filter_map(|edit| edit.agent.clone()) {
                if !by.contains(&agent) {
                    by.push(agent);
                }
            }
            let newest = edits.iter().map(|edit| edit.seq).max().unwrap_or_default();
            let traces = edits
                .iter()
                .filter_map(|edit| {
                    let token = edit.r#ref.clone()?;
                    let at = shown_at(session, edit);
                    let trace = Trace {
                        token,
                        glyph: Glyph::Pencil,
                        label: path.clone(),
                        detail: format!("edited at {at}"),
                    };
                    Some((format!("Mention the edit at {at}").into(), trace))
                })
                .collect();
            let file = EditedFile {
                path: path.clone().into(),
                edits: shown,
                by,
            };
            (newest, file, traces)
        })
        .collect();
    files.sort_by_key(|(newest, ..)| std::cmp::Reverse(*newest));
    files
        .into_iter()
        .map(|(_, file, traces)| (file, traces))
        .collect()
}

impl FileEdits {
    pub fn count(&self) -> usize {
        self.files.len()
    }

    fn rebuild(&mut self, cx: &mut Context<Self>) {
        (self.files, self.traces) = self
            .store
            .read(cx)
            .open_session()
            .map_or_else(Vec::new, files)
            .into_iter()
            .unzip();
    }

    pub fn counted(&mut self, cx: &mut Context<Self>) -> String {
        self.rebuild(cx);
        let stored: usize = self
            .store
            .read(cx)
            .open_session()
            .map_or(0, |s| s.files.values().map(Vec::len).sum());
        let shown: usize = self.files.iter().map(|file| file.edits.len()).sum();
        format!("{} files, {stored} edits, {shown} shown", self.files.len())
    }
}

fn cassette_edits() -> usize {
    include_str!("../../../../../cassettes/36-agents.cassette")
        .lines()
        .filter(|line| line.contains("\"method\": \"file.edit\""))
        .count()
}

impl Render for FileEdits {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let (opener, closer) = (cx.weak_entity(), cx.weak_entity());
        let open = move |at: usize, _: &mut Window, cx: &mut App| {
            opener
                .update(cx, |module, cx| {
                    module.opened = at;
                    module.drawer = true;
                    cx.notify();
                })
                .ok();
        };
        let history = self
            .files
            .get(self.opened)
            .map(|file| file_history(("file-history", self.opened), file, &theme));
        let mentions = self
            .traces
            .get(self.opened)
            .into_iter()
            .flatten()
            .enumerate()
            .map(|(at, (words, trace))| {
                let trace = trace.clone();
                chip::trace(("file-edit-mention", at), words.clone(), &theme)
                    .on_click(move |_, window, cx| mention(trace.clone(), window, cx))
            });
        div()
            .relative()
            .size_full()
            .child(file_edits("file-edits", &self.files, &theme, open))
            .child(
                Drawer::new("file-edits-drawer")
                    .open(self.drawer && history.is_some())
                    .on_close(move |_, cx| {
                        closer
                            .update(cx, |module, cx| {
                                module.drawer = false;
                                cx.notify();
                            })
                            .ok();
                    })
                    .child(
                        div()
                            .id("file-history-scroll")
                            .size_full()
                            .overflow_y_scroll()
                            .child(div().flex().flex_wrap().gap_2().p_3().children(mentions))
                            .children(history),
                    ),
            )
    }
}
