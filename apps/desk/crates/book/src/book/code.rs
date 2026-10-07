use std::cell::Cell;
use std::ops::Range;
use std::rc::Rc;

use desk_core::buffer::Buffer;
use desk_core::syntax::{Kind, Language, Syntax, SyntaxError};
use desk_ui::components::code::{CodeLine, code_view};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{Div, UniformListScrollHandle, Window, div, point, prelude::*, px};

use super::kit::{block, label, spread};

const CODE_WIDTH: f32 = 720.0;
const CODE_HEIGHT: f32 = 706.0;
const NARROW: f32 = 320.0;
const NARROW_HEIGHT: f32 = 220.0;
const SWATCH: f32 = 10.0;
const SIDEWAYS_STEP: f32 = 120.0;
const CODE_PATH: &str = "ui/src/components/form.rs";
const CODE_TEXT: &str = include_str!("../../../ui/src/components/form.rs");
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

fn syntax_token(kind: Kind) -> ColorToken {
    match kind {
        Kind::Keyword => ColorToken::SyntaxKeyword,
        Kind::String => ColorToken::SyntaxString,
        Kind::Number => ColorToken::SyntaxNumber,
        Kind::Comment => ColorToken::SyntaxComment,
        Kind::Function => ColorToken::SyntaxFunction,
        Kind::Type => ColorToken::SyntaxType,
        Kind::Variable => ColorToken::SyntaxVariable,
        Kind::Constant => ColorToken::SyntaxConstant,
        Kind::Operator => ColorToken::SyntaxOperator,
        Kind::Punctuation => ColorToken::SyntaxPunctuation,
    }
}

fn byte_range(text: &str, chars: Range<usize>) -> Range<usize> {
    let byte = |char: usize| {
        text.char_indices()
            .nth(char)
            .map_or(text.len(), |(at, _)| at)
    };
    byte(chars.start)..byte(chars.end)
}

fn coloured(text: &str) -> Result<Vec<CodeLine>, SyntaxError> {
    let buffer = Buffer::from_text(text);
    let syntax = Syntax::new(Language::Rust, &buffer)?;
    let count = buffer.line_count();
    let mut lines: Vec<CodeLine> = (0..count)
        .map(|row| CodeLine {
            text: buffer.line(row).unwrap_or_default().into(),
            runs: Vec::new(),
        })
        .collect();
    for span in syntax.spans(0..count) {
        let mut at = span.chars.start;
        while at < span.chars.end {
            let Ok(row) = buffer.char_to_line(at) else {
                break;
            };
            let Ok(start) = buffer.line_to_char(row) else {
                break;
            };
            let end = buffer
                .line_to_char(row + 1)
                .map_or(span.chars.end, |next| next.min(span.chars.end));
            if let Some(line) = lines.get_mut(row) {
                let bytes = byte_range(&line.text, at - start..end - start);
                if !bytes.is_empty() {
                    line.runs.push((bytes, syntax_token(span.kind)));
                }
            }
            at = end;
        }
    }
    Ok(lines)
}

struct CodeSample {
    lines: Rc<[CodeLine]>,
    widest: usize,
    wide: UniformListScrollHandle,
    narrow: UniformListScrollHandle,
    built: Rc<Cell<usize>>,
    shown: usize,
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
        let view = code_view("code-sample", lines.len(), &self.wide, move |row| {
            counter.set(counter.get() + 1);
            lines.get(row).cloned().unwrap_or_default()
        })
        .widest(self.widest);
        let lines = self.lines.clone();
        let narrow = code_view("code-narrow", lines.len(), &self.narrow, move |row| {
            lines.get(row).cloned().unwrap_or_default()
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
