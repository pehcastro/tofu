use std::cell::{Cell, OnceCell, RefCell};
use std::fs;
use std::path::Path;
use std::rc::Rc;

use desk_core::buffer::{Buffer, BufferError};
use desk_core::syntax::{Kind, Language, Syntax, SyntaxError};
use desk_ui::components::code::{CodeLine, code_view};
use desk_ui::components::code_editor::{CodeEditor, code_lines, syntax_token};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::Theme;
use gpui::{
    App, AppContext, Div, Entity, SharedString, UniformListScrollHandle, Window, div, point,
    prelude::*, px,
};

use super::kit::{block, label, spread};

const CODE_WIDTH: f32 = 720.0;
const CODE_HEIGHT: f32 = 706.0;
const EDITOR_HEIGHT: f32 = 440.0;
const NARROW: f32 = 320.0;
const NARROW_HEIGHT: f32 = 220.0;
const SWATCH: f32 = 10.0;
const SIDEWAYS_STEP: f32 = 120.0;
const CODE_PATH: &str = "ui/src/components/form.rs";
const CODE_TEXT: &str = include_str!("../../../ui/src/components/form.rs");
const SCRATCH_SHOWN: &str = ".local/desk-app/shots/desk-163/form.rs";
const SCRATCH: &str = concat!(
    env!("CARGO_MANIFEST_DIR"),
    "/../../../../.local/desk-app/shots/desk-163/form.rs"
);
const KINDS: [Kind; 10] = [
    Kind::Keyword,
    Kind::String,
    Kind::Number,
    Kind::Comment,
    Kind::Function,
    Kind::Type,
    Kind::Variable,
    Kind::Constant,
    Kind::Operator,
    Kind::Punctuation,
];

fn coloured(text: &str) -> Result<Vec<CodeLine>, SyntaxError> {
    let buffer = Buffer::from_text(text);
    let syntax = Syntax::new(Language::Rust, &buffer)?;
    Ok(code_lines(&buffer, &syntax, 0..buffer.line_count()))
}

type Opened = Result<Entity<CodeEditor>, SyntaxError>;

fn open(saved: Rc<RefCell<SharedString>>, cx: &mut App) -> Opened {
    let buffer = Buffer::from_text(CODE_TEXT);
    let syntax = Syntax::new(Language::Rust, &buffer)?;
    Ok(cx.new(|cx| {
        CodeEditor::new(buffer, syntax, cx).on_save(move |buffer, _, _| {
            let path = Path::new(SCRATCH);
            let written = path
                .parent()
                .map_or(Ok(()), fs::create_dir_all)
                .map_err(BufferError::from)
                .and_then(|()| buffer.save(path));
            *saved.borrow_mut() = match written {
                Ok(()) => format!("saved {} lines to {SCRATCH_SHOWN}", buffer.line_count()),
                Err(error) => format!("save failed: {error}"),
            }
            .into();
        })
    }))
}

fn readout(editor: &CodeEditor, saved: &SharedString) -> Vec<String> {
    let buffer = editor.buffer();
    let mut lines = vec![format!(
        "dirty {} | carets {} | last save: {}{}",
        buffer.is_dirty(),
        editor.carets().count(),
        if saved.is_empty() { "none" } else { saved },
        editor
            .failure()
            .map_or(String::new(), |failure| format!(" | failure: {failure}")),
    )];
    for (index, (anchor, head)) in editor.carets().enumerate() {
        let row = buffer.char_to_line(head).unwrap_or_default();
        let start = buffer.line_to_char(row).unwrap_or_default();
        lines.push(format!(
            "caret {}: line {} col {} char {head} selected {} | {}",
            index + 1,
            row + 1,
            head - start + 1,
            head.abs_diff(anchor),
            buffer.line(row).unwrap_or_default(),
        ));
    }
    if let Some(row) = editor
        .carets()
        .next()
        .and_then(|(_, head)| buffer.char_to_line(head).ok())
    {
        let runs: Vec<String> = editor
            .syntax()
            .spans(row..row + 1)
            .into_iter()
            .filter_map(|span| {
                let text = buffer.rope().get_slice(span.chars)?;
                Some(format!("{} `{text}`", span.kind.name()))
            })
            .collect();
        lines.push(format!("line {} colours: {}", row + 1, runs.join(", ")));
    }
    lines
}

#[derive(IntoElement)]
struct EditorBlock {
    editor: Rc<OnceCell<Opened>>,
    saved: Rc<RefCell<SharedString>>,
}

impl RenderOnce for EditorBlock {
    fn render(self, _: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let saved = self.saved.clone();
        let editor = match self.editor.get_or_init(|| open(saved, cx)) {
            Ok(editor) => editor.clone(),
            Err(error) => return label(error.to_string(), &theme),
        };
        let shown = readout(editor.read(cx), &self.saved.borrow());
        div()
            .flex()
            .flex_col()
            .gap_3()
            .child(block(
                "editable: click, type, Alt click adds a caret, Ctrl Z, Ctrl S saves a scratch copy",
                &theme,
                div().w(px(CODE_WIDTH)).h(px(EDITOR_HEIGHT)).child(editor),
            ))
            .children(shown.into_iter().map(|line| label(line, &theme)))
    }
}

struct CodeSample {
    lines: Rc<[CodeLine]>,
    widest: usize,
    wide: UniformListScrollHandle,
    narrow: UniformListScrollHandle,
    built: Rc<Cell<usize>>,
    shown: usize,
    editor: Rc<OnceCell<Opened>>,
    saved: Rc<RefCell<SharedString>>,
}

impl CodeSample {
    fn new() -> Result<Self, SyntaxError> {
        let lines: Rc<[CodeLine]> = coloured(CODE_TEXT)?.into();
        let widest = lines
            .iter()
            .enumerate()
            .max_by_key(|(_, line)| line.text.chars().count())
            .map_or(0, |(row, _)| row);
        Ok(CodeSample {
            lines,
            widest,
            wide: UniformListScrollHandle::new(),
            narrow: UniformListScrollHandle::new(),
            built: Rc::default(),
            shown: 0,
            editor: Rc::default(),
            saved: Rc::default(),
        })
    }

    fn sideways(&self, by: f32) {
        for scroll in [&self.wide, &self.narrow] {
            let base = scroll.0.borrow().base_handle.clone();
            let offset = base.offset();
            let x = (offset.x - px(by)).clamp(-base.max_offset().x, px(0.0));
            base.set_offset(point(x, offset.y));
        }
    }

    fn render(&mut self, theme: &Theme, window: &mut Window) -> Div {
        let built = self.built.replace(0);
        if built != self.shown {
            self.shown = built;
            window.request_animation_frame();
        }
        let (lines, counter) = (self.lines.clone(), self.built.clone());
        let view = code_view("code-sample", lines.len(), &self.wide, move |rows, _| {
            counter.set(counter.get() + rows.len());
            rows.map(|row| lines.get(row).cloned().unwrap_or_default())
                .collect()
        })
        .widest(self.widest);
        let lines = self.lines.clone();
        let narrow = code_view("code-narrow", lines.len(), &self.narrow, move |rows, _| {
            rows.map(|row| lines.get(row).cloned().unwrap_or_default())
                .collect()
        })
        .widest(self.widest);
        let legend = spread(theme).children(KINDS.map(|kind| {
            div()
                .flex()
                .items_center()
                .gap_1()
                .child(
                    div()
                        .size(px(SWATCH))
                        .rounded_sm()
                        .bg(theme.color(syntax_token(kind))),
                )
                .child(label(kind.name(), theme))
        }));
        div()
            .flex()
            .flex_col()
            .gap_3()
            .child(label(
                format!(
                    "Code view: {CODE_PATH}, {} lines coloured by desk_core::syntax, rows built last frame {}; left and right scroll sideways",
                    self.lines.len(),
                    self.shown
                ),
                theme,
            ))
            .child(legend)
            .child(EditorBlock {
                editor: self.editor.clone(),
                saved: self.saved.clone(),
            })
            .child(block(
                "board width",
                theme,
                div().w(px(CODE_WIDTH)).h(px(CODE_HEIGHT)).child(view),
            ))
            .child(block(
                "320 px",
                theme,
                div().w(px(NARROW)).h(px(NARROW_HEIGHT)).child(narrow),
            ))
    }
}

pub(super) struct CodePage {
    sample: Result<CodeSample, SyntaxError>,
}

impl CodePage {
    pub(super) fn new() -> Self {
        CodePage {
            sample: CodeSample::new(),
        }
    }

    pub(super) fn render(&mut self, theme: &Theme, window: &mut Window) -> Div {
        match &mut self.sample {
            Ok(sample) => sample.render(theme, window),
            Err(error) => label(error.to_string(), theme),
        }
    }

    pub(super) fn key(&mut self, key: &str) -> bool {
        let Ok(sample) = &self.sample else {
            return false;
        };
        match key {
            "right" => sample.sideways(SIDEWAYS_STEP),
            "left" => sample.sideways(-SIDEWAYS_STEP),
            _ => return false,
        }
        true
    }
}
