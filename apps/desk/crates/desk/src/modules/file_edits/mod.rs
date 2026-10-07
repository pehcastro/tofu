use super::chat::cassette;

use std::borrow::Cow;
use std::rc::Rc;

use desk_core::bridge::Event;
use desk_core::model::{Session, Store};
use desk_core::protocol::{FileEdit as Edit, FileEditOp, HunkLineKind};
use desk_ui::components::card::{Header, inner_card, shell};
use desk_ui::components::diff::{FileChange, FileDiff};
use desk_ui::components::file_edits::{EditedFile, FileEdit, file_edits, file_history};
use desk_ui::components::glyph::Glyph;
use desk_ui::live::ActiveTheme;
use gpui::{
    AnyView, App, AppContext, Context, IntoElement, Render, SharedString, Window, div, prelude::*,
    px,
};

use cassette::{Replay, Step};

const BOARD: &str = "36-agents";
const TILE_WIDTH: f32 = 513.0;
const HISTORY_WIDTH: f32 = 495.0;
const INSET: f32 = 8.0;
const FONTS: [&[u8]; 6] = [
    include_bytes!("../../../../../assets/fonts/Geist-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-SemiBold.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-SemiBold.ttf"),
];

struct FileEdits {
    store: Store,
    replay: Option<Replay>,
    files: Vec<EditedFile>,
    opened: usize,
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if board.is_some_and(|board| board != BOARD) {
        return Err(format!(
            "the file edits module replays {BOARD}, not {board:?}"
        ));
    }
    cx.text_system()
        .add_fonts(FONTS.iter().map(|font| Cow::Borrowed(*font)).collect())
        .map_err(|error| format!("the file edits module cannot load the Geist fonts: {error}"))?;
    let replay = Replay::read()?;
    Ok(cx
        .new(|_| FileEdits {
            store: Store::default(),
            replay: Some(replay),
            files: Vec::new(),
            opened: 0,
        })
        .into())
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
    let at = session
        .tools
        .get(&edit.item)
        .and_then(|tool| tool.started_at.as_deref()?.get(11..19))
        .map_or_else(|| format!("seq {}", edit.seq), str::to_owned);
    Ok(FileEdit {
        agent,
        at: at.into(),
        diff: Rc::new(diff),
    })
}

fn files(session: &Session) -> Vec<EditedFile> {
    let mut files: Vec<(i64, EditedFile)> = session
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
            let file = EditedFile {
                path: path.clone().into(),
                edits: shown,
                by,
            };
            (newest, file)
        })
        .collect();
    files.sort_by_key(|(newest, _)| std::cmp::Reverse(*newest));
    files.into_iter().map(|(_, file)| file).collect()
}

impl FileEdits {
    fn session(&self) -> Option<&Session> {
        self.store.sessions.values().next()
    }

    fn rebuild(&mut self) {
        self.files = self.session().map_or_else(Vec::new, files);
    }

    fn feed(&mut self, event: &Event) {
        if let Err(error) = self.store.apply_batch(std::slice::from_ref(event)) {
            eprintln!("desk: the store refused an event from the cassette: {error}");
        }
    }

    fn settle(&mut self) -> Result<(), String> {
        self.replay = None;
        self.store = Store::default();
        let mut whole = Replay::read()?;
        while let Step::Feed(event) = whole.step()? {
            self.feed(&event);
        }
        self.rebuild();
        let stored = self
            .session()
            .map_or(0, |s| s.files.values().map(Vec::len).sum());
        let shown: usize = self.files.iter().map(|file| file.edits.len()).sum();
        eprintln!(
            "desk: file edits from the store: {} files, {stored} edits, {shown} shown; cassette: {} file.edit events",
            self.files.len(),
            cassette_edits()
        );
        Ok(())
    }

    fn frame(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(replay) = &mut self.replay else {
            return;
        };
        window.request_animation_frame();
        let stepped = match replay.step() {
            Ok(Step::Feed(event)) => {
                self.feed(&event);
                self.rebuild();
                Ok(())
            }
            Ok(Step::Restart) => {
                self.store = Store::default();
                Ok(())
            }
            Ok(Step::Report) => self.settle(),
            Err(error) => Err(error),
        };
        if let Err(error) = stepped {
            eprintln!("desk: the cassette has a bad line: {error}");
            cx.quit();
        }
    }
}

fn cassette_edits() -> usize {
    include_str!("../../../../../cassettes/36-agents.cassette")
        .lines()
        .filter(|line| line.contains("\"method\": \"file.edit\""))
        .count()
}

impl Render for FileEdits {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        self.frame(window, cx);
        let theme = ActiveTheme::theme(cx);
        let this = cx.weak_entity();
        let open = move |ix: usize, _: &mut Window, cx: &mut App| {
            this.update(cx, |module, cx| {
                module.opened = ix;
                cx.notify();
            })
            .ok();
        };
        let tile = shell(
            Header::Title(Some(Glyph::File), "File edits".into(), None),
            &theme,
        )
        .w(px(TILE_WIDTH))
        .h_full()
        .child(inner_card(&theme).flex_1().min_h_0().child(file_edits(
            "file-edits",
            &self.files,
            &theme,
            open,
        )));
        let history = self.files.get(self.opened).map(|file| {
            div()
                .id("file-history-scroll")
                .w(px(HISTORY_WIDTH))
                .h_full()
                .flex_none()
                .overflow_y_scroll()
                .child(file_history(("file-history", self.opened), file, &theme))
        });
        let meter = self.replay.as_ref().map(Replay::meter);
        div()
            .size_full()
            .flex()
            .items_start()
            .justify_center()
            .gap_3()
            .p(px(INSET))
            .child(tile)
            .children(history)
            .children(meter)
    }
}
