use std::cell::{Cell, RefCell};
use std::collections::BTreeMap;
use std::fmt::Display;
use std::fs;
use std::hash::{DefaultHasher, Hasher};
use std::ops::Range;
use std::path::{Path, PathBuf};
use std::rc::Rc;
use std::sync::Arc;
use std::time::{Duration, Instant};

use desk_core::buffer::Buffer;
use desk_core::symbols::{Index, Outline, Symbol};
use desk_core::syntax::{Kind, Language, Span, Syntax};
use gpui::{
    AnyElement, App, Bounds, ClipboardItem, Context, ElementInputHandler, Entity,
    EntityInputHandler, FocusHandle, Focusable, KeyDownEvent, Keystroke, ModifiersChangedEvent,
    MouseButton, MouseDownEvent, MouseMoveEvent, MouseUpEvent, Pixels, Point, SharedString, Task,
    UTF16Selection, UniformListScrollHandle, WeakEntity, Window, canvas, div, fill, point,
    prelude::*, px, size,
};

use crate::components::chat::fail;
use crate::components::code::{
    Blank, CodeLine, FoldMark, Guides, Heading, Heights, Marks, Stripe, TAB_STOP, Wrap,
    code_origin, code_place, code_shape, code_view, reveal_row, wrap_width,
};
use crate::components::find::{FindBar, Matching, Replace, find_matching};
use crate::components::form::{blinker, caret_shown};
use crate::components::keymap::{Action, FocusedKeys, Host, Keymap, Motion, Resolved, Stroke};
use crate::components::paint::ink;
use crate::components::size::{CARET_WIDTH, NUMBER_COLUMN, NUMBER_PAD, T3};
use crate::components::vim::{self, Mode, Vim};
use crate::live::ActiveTheme;
use crate::theme::{ColorToken, Theme};
use desk_motion::tokens::EASE_OUT;

mod markdown;

const TAB: &str = "    ";
const BARE_PAD: f32 = 12.0;
const FAILURE_INSET: f32 = 12.0;
const CURRENT_LINE: ColorToken = ColorToken::StateHover;
pub const READ_ONLY_BYTES: u64 = 1024 * 1024;
const REFUSED_BYTES: u64 = 64 * 1024 * 1024;
const LONG_LINE_BYTES: usize = 64 * 1024;
const SNIFF_BYTES: usize = 8192;
const CONTROL_SHARE: f32 = 0.3;
const PROSE: [&str; 4] = ["md", "mdx", "txt", "rst"];
const LINE_COMMENTS: [(&str, &str); 26] = [
    ("rs", "//"),
    ("go", "//"),
    ("ts", "//"),
    ("tsx", "//"),
    ("mts", "//"),
    ("cts", "//"),
    ("js", "//"),
    ("jsx", "//"),
    ("mjs", "//"),
    ("cjs", "//"),
    ("c", "//"),
    ("h", "//"),
    ("cpp", "//"),
    ("cs", "//"),
    ("java", "//"),
    ("kt", "//"),
    ("swift", "//"),
    ("jsonc", "//"),
    ("py", "#"),
    ("toml", "#"),
    ("sh", "#"),
    ("bash", "#"),
    ("yaml", "#"),
    ("yml", "#"),
    ("ps1", "#"),
    ("sql", "--"),
];
const PILL_GAP: f32 = 6.0;
const PILL_PAD: f32 = 6.0;
const PILL_HEIGHT: f32 = 16.0;
const PILL_RADIUS: f32 = 4.0;
const LINK_DROP: f32 = 4.0;
const LINK_THICKNESS: f32 = 1.0;
const LINK: ColorToken = ColorToken::SyntaxLinkUri;
const GUIDE_REACH: usize = 1000;
const FIRST_SCREEN_LINES: usize = 128;
const STRIPE_SOLID: Duration = Duration::from_secs(6);
const STRIPE_FADE: Duration = Duration::from_secs(2);
const STRIPE: ColorToken = ColorToken::StatusAccent;
const OUTLINE_SETTLE: Duration = Duration::from_millis(300);

struct Wrote {
    lines: Range<usize>,
    at: Instant,
    unlogged: Option<(String, Instant)>,
    _fade: Task<()>,
}

fn bare(chars: impl Iterator<Item = char>) -> impl Iterator<Item = char> {
    chars.filter(|ch| !matches!(ch, '\n' | '\r'))
}

struct Rest {
    path: PathBuf,
    _colouring: Option<Task<()>>,
}

pub fn open_phase(name: &str, started: Instant) {
    eprintln!(
        "desk: editor open phase {name} {:.1} ms",
        started.elapsed().as_secs_f64() * 1000.0
    );
}
const PAINT_SAMPLES: usize = 120;

fn indent_levels(chars: impl Iterator<Item = char>) -> Option<usize> {
    let mut columns = 0;
    for ch in chars {
        match ch {
            ' ' => columns += 1,
            '\t' => columns += TAB_STOP - columns % TAB_STOP,
            '\r' | '\n' => return None,
            _ => return Some(columns.div_ceil(TAB_STOP)),
        }
    }
    None
}

#[derive(Default)]
struct PaintFrames {
    started: Cell<Option<Instant>>,
    samples: RefCell<Vec<f64>>,
}

impl PaintFrames {
    fn start(&self) {
        self.started.set(Some(Instant::now()));
    }

    fn end(&self) {
        let Some(started) = self.started.take() else {
            return;
        };
        let mut samples = self.samples.borrow_mut();
        samples.push(started.elapsed().as_secs_f64() * 1000.0);
        if samples.len() < PAINT_SAMPLES {
            return;
        }
        samples.sort_by(f64::total_cmp);
        if let Some(median) = samples.get(samples.len() / 2) {
            eprintln!("desk: editor guides paint {median:.3} ms");
        }
        samples.clear();
    }
}

pub struct Asked {
    pub name: String,
    pub at: Point<Pixels>,
    pub row: usize,
    pub col: usize,
    pub here: Vec<Symbol>,
    pub since: Instant,
}

pub enum Way {
    Back,
    Forward,
}

pub enum Jump {
    Definition(Asked),
    Travel { way: Way, row: usize, col: usize },
}

enum Outlined {
    Stale,
    Building { _outlining: Task<()> },
    Built(Outline),
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum FileKind {
    Text,
    Large(u64),
    Binary(u64),
}

pub enum Highlight {
    Plain,
    Tree(Syntax),
}

impl Highlight {
    pub fn spans(&self, rows: Range<usize>) -> Vec<Span> {
        match self {
            Highlight::Plain => Vec::new(),
            Highlight::Tree(syntax) => syntax.spans(rows),
        }
    }
}

fn binary(head: &[u8]) -> bool {
    let control = head
        .iter()
        .filter(|byte| byte.is_ascii_control() && !matches!(byte, b'\t' | b'\n' | b'\r' | 0x0c))
        .count();
    head.contains(&0) || !head.is_empty() && control as f32 / head.len() as f32 > CONTROL_SHARE
}

fn cut(text: &str, most: usize) -> &str {
    let end = (0..=most.min(text.len()))
        .rev()
        .find(|at| text.is_char_boundary(*at))
        .unwrap_or(0);
    text.get(..end).unwrap_or_default()
}

pub fn syntax_token(kind: Kind) -> ColorToken {
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
        Kind::Attribute => ColorToken::SyntaxAttribute,
        Kind::Boolean => ColorToken::SyntaxBoolean,
        Kind::CommentDoc => ColorToken::SyntaxCommentDoc,
        Kind::Constructor => ColorToken::SyntaxConstructor,
        Kind::Embedded => ColorToken::SyntaxEmbedded,
        Kind::Emphasis => ColorToken::SyntaxEmphasis,
        Kind::EmphasisStrong => ColorToken::SyntaxEmphasisStrong,
        Kind::Enum => ColorToken::SyntaxEnum,
        Kind::Label => ColorToken::SyntaxLabel,
        Kind::LinkText => ColorToken::SyntaxLinkText,
        Kind::LinkUri => ColorToken::SyntaxLinkUri,
        Kind::Namespace => ColorToken::SyntaxNamespace,
        Kind::Preproc => ColorToken::SyntaxPreproc,
        Kind::Property => ColorToken::SyntaxProperty,
        Kind::PunctuationBracket => ColorToken::SyntaxPunctuationBracket,
        Kind::PunctuationDelimiter => ColorToken::SyntaxPunctuationDelimiter,
        Kind::PunctuationListMarker => ColorToken::SyntaxPunctuationListMarker,
        Kind::PunctuationMarkup => ColorToken::SyntaxPunctuationMarkup,
        Kind::PunctuationSpecial => ColorToken::SyntaxPunctuationSpecial,
        Kind::Selector => ColorToken::SyntaxSelector,
        Kind::SelectorPseudo => ColorToken::SyntaxSelectorPseudo,
        Kind::StringEscape => ColorToken::SyntaxStringEscape,
        Kind::StringRegex => ColorToken::SyntaxStringRegex,
        Kind::StringSpecial => ColorToken::SyntaxStringSpecial,
        Kind::StringSpecialSymbol => ColorToken::SyntaxStringSpecialSymbol,
        Kind::Tag => ColorToken::SyntaxTag,
        Kind::TextLiteral => ColorToken::SyntaxTextLiteral,
        Kind::Title => ColorToken::SyntaxTitle,
        Kind::VariableParameter => ColorToken::SyntaxVariableParameter,
        Kind::VariableSpecial => ColorToken::SyntaxVariableSpecial,
        Kind::Variant => ColorToken::SyntaxVariant,
    }
}

pub fn code_lines(buffer: &Buffer, syntax: &Syntax, rows: Range<usize>) -> Vec<CodeLine> {
    coloured_lines(buffer, syntax.spans(rows.clone()), rows)
        .into_iter()
        .map(|(line, _)| line)
        .collect()
}

fn prose(path: &Path) -> bool {
    path.extension()
        .and_then(|extension| extension.to_str())
        .is_none_or(|extension| {
            PROSE
                .iter()
                .any(|prose| extension.eq_ignore_ascii_case(prose))
        })
}

struct Drawn {
    text: String,
    starts: Vec<usize>,
    heading: Heading,
}

impl Drawn {
    fn new(line: &str, hidden: &[Range<usize>]) -> Self {
        let mut text = String::new();
        let mut starts = Vec::new();
        let mut columns = 0;
        for (column, ch) in line.chars().enumerate() {
            starts.push(text.len());
            if hidden.iter().any(|range| range.contains(&column)) {
                continue;
            }
            let width = match ch {
                '\t' => TAB_STOP - columns % TAB_STOP,
                _ => 1,
            };
            match ch {
                '\t' => text.extend(std::iter::repeat_n(' ', width)),
                _ => text.push(ch),
            }
            columns += width;
        }
        starts.push(text.len());
        Self {
            text,
            starts,
            heading: Heading::Body,
        }
    }

    fn columns(&self) -> usize {
        self.starts.len().saturating_sub(1)
    }

    fn byte(&self, column: usize) -> usize {
        self.starts.get(column).copied().unwrap_or(self.text.len())
    }

    fn column(&self, byte: usize) -> usize {
        let after = self.starts.partition_point(|start| *start < byte);
        match after.checked_sub(1) {
            Some(before) if byte - self.byte(before) < self.byte(after).saturating_sub(byte) => {
                before
            }
            _ => {
                let landed = self.byte(after);
                let last = self.starts.partition_point(|start| *start <= landed);
                last.saturating_sub(1).max(after).min(self.columns())
            }
        }
    }
}

fn coloured_lines(buffer: &Buffer, spans: Vec<Span>, rows: Range<usize>) -> Vec<(CodeLine, Drawn)> {
    let rope = buffer.rope();
    let mut long = Vec::new();
    let mut lines: Vec<(CodeLine, Drawn)> = rows
        .clone()
        .map(|row| {
            let full = buffer.line(row).unwrap_or_default();
            long.push(full.len() > LONG_LINE_BYTES);
            let drawn = Drawn::new(cut(&full, LONG_LINE_BYTES), &[]);
            let line = CodeLine {
                text: drawn.text.clone().into(),
                ..CodeLine::default()
            };
            (line, drawn)
        })
        .collect();
    let last_row = rows.end.saturating_sub(1);
    for span in spans {
        let (Ok(first), Ok(last)) = (
            buffer.char_to_line(span.chars.start),
            buffer.char_to_line(span.chars.end),
        ) else {
            continue;
        };
        for row in first.max(rows.start)..=last.min(last_row) {
            let at = row - rows.start;
            let (Some((line, drawn)), Ok(base), Some(false)) =
                (lines.get_mut(at), rope.try_line_to_char(row), long.get(at))
            else {
                continue;
            };
            let bytes = drawn.byte(span.chars.start.saturating_sub(base))
                ..drawn.byte(span.chars.end.saturating_sub(base));
            if !bytes.is_empty() {
                line.runs.push((bytes, syntax_token(span.kind)));
            }
        }
    }
    lines
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
struct Caret {
    anchor: usize,
    head: usize,
    goal: Option<usize>,
}

impl Caret {
    fn at(char: usize) -> Self {
        Caret {
            anchor: char,
            head: char,
            goal: None,
        }
    }

    fn range(self) -> Range<usize> {
        self.anchor.min(self.head)..self.anchor.max(self.head)
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub(crate) enum Class {
    Space,
    Word,
    Mark,
}

pub(crate) fn class(ch: char) -> Class {
    if ch.is_whitespace() {
        Class::Space
    } else if ch.is_alphanumeric() || ch == '_' {
        Class::Word
    } else {
        Class::Mark
    }
}

fn chars_within(text: &str, units: usize) -> usize {
    let mut seen = 0;
    text.chars()
        .take_while(|ch| {
            let before = seen;
            seen += ch.len_utf16();
            before < units
        })
        .count()
}

#[derive(Clone, Copy)]
struct LineEdit {
    line: usize,
    first: usize,
    gone: usize,
    added: usize,
}

impl LineEdit {
    fn new(line: usize, line_start: bool, removed: &str, inserted: &str) -> Self {
        let whole_lines = |text: &str| text.is_empty() || text.ends_with('\n');
        let whole = line_start && whole_lines(removed) && whole_lines(inserted);
        LineEdit {
            line,
            first: line + usize::from(!whole),
            gone: removed.matches('\n').count(),
            added: inserted.matches('\n').count(),
        }
    }

    fn row(self, row: usize) -> Option<usize> {
        match row {
            row if row < self.first => Some(row),
            row if row < self.first + self.gone => None,
            row => Some(row - self.gone + self.added),
        }
    }

    fn touches(self, lines: &Range<usize>) -> bool {
        self.line < lines.end && lines.start <= self.line + self.gone
    }

    fn follow<T: Clone>(self, rows: &BTreeMap<usize, T>) -> BTreeMap<usize, T> {
        rows.iter()
            .filter_map(|(row, value)| Some((self.row(*row)?, value.clone())))
            .collect()
    }
}

fn fold_pill(header: usize, editor: WeakEntity<CodeEditor>, theme: &Theme) -> AnyElement {
    div()
        .id(("fold-pill", header))
        .flex_none()
        .ml(px(PILL_GAP))
        .px(px(PILL_PAD))
        .h(px(PILL_HEIGHT))
        .flex()
        .items_center()
        .rounded(px(PILL_RADIUS))
        .bg(theme.color(ColorToken::StateHover))
        .text_color(ink(theme, T3))
        .cursor_pointer()
        .child("…")
        .on_mouse_down(MouseButton::Left, move |_, _, cx| {
            cx.stop_propagation();
            let unfolded = editor.update(cx, |editor, cx| {
                editor.toggle_fold(header);
                cx.notify();
            });
            if let Err(error) = unfolded {
                eprintln!("desk: editor is gone before its unfold: {error}");
            }
        })
        .into_any_element()
}

type Save = Rc<dyn Fn(&mut Buffer, &mut Window, &mut App) -> Result<(), String>>;
type OnJump = Rc<dyn Fn(Jump, &mut Window, &mut App)>;

pub struct CodeEditor {
    buffer: Buffer,
    syntax: Highlight,
    kind: FileKind,
    carets: Vec<Caret>,
    marked: Option<Range<usize>>,
    widest: usize,
    dragging: bool,
    failure: Option<(&'static str, SharedString)>,
    marks: Rc<Marks>,
    scroll: UniformListScrollHandle,
    bounds: Rc<Cell<Bounds<Pixels>>>,
    focus: FocusHandle,
    on_save: Option<Save>,
    since: Instant,
    blink: Option<Task<()>>,
    bar: Option<Entity<FindBar>>,
    query: String,
    found: Vec<Range<usize>>,
    current: usize,
    edited_since_found: bool,
    wrapping: bool,
    wrap: Rc<Wrap>,
    wrap_stale: Stale,
    wrap_reveal: Vec<usize>,
    gutter: bool,
    live: bool,
    parsed: markdown::Parsed,
    heights: Heights,
    heights_stale: Stale,
    colour_stale: bool,
    frames: Option<Rc<RefCell<TypingFrames>>>,
    frames_flush: Option<Task<()>>,
    folds: BTreeMap<usize, usize>,
    shown_lines: Option<Rc<[usize]>>,
    chevron: Option<usize>,
    folded_at: Rc<Cell<Option<Instant>>>,
    keymap: Rc<Keymap>,
    comment: Option<&'static str>,
    vim: Option<Vim>,
    language: Option<Language>,
    symbols: Option<Arc<Index>>,
    path: Option<Arc<Path>>,
    outlined: Outlined,
    link: Option<Range<usize>>,
    on_jump: Option<OnJump>,
    guides: bool,
    active_guide: Option<(usize, Range<usize>)>,
    paints: Option<Rc<PaintFrames>>,
    rest: Option<Rest>,
    wrote: Option<Wrote>,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Stale {
    Fresh,
    Lines { at: usize, old: usize, new: usize },
    Whole,
}

impl Stale {
    fn then(self, next: Stale) -> Stale {
        match (self, next) {
            (Stale::Whole, _) | (_, Stale::Whole) => Stale::Whole,
            (Stale::Fresh, next) => next,
            (done, Stale::Fresh) => done,
            (
                Stale::Lines {
                    at: first_at,
                    old: first_old,
                    new: first_new,
                },
                Stale::Lines { at, old, new },
            ) => {
                let start = first_at.min(at);
                let end = (first_at + first_new).max(at + old);
                Stale::Lines {
                    at: start,
                    old: end - first_new + first_old - start,
                    new: end - old + new - start,
                }
            }
        }
    }
}

#[derive(Clone, Copy, Default)]
struct Phases {
    wait: Option<f64>,
    render: f64,
    passes: f64,
    rows: f64,
}

#[derive(Default)]
struct TypingFrames {
    edited: Option<Instant>,
    pending: Phases,
    intervals: Vec<(f64, Phases)>,
    mode: &'static str,
}

fn ms_since(at: Instant) -> f64 {
    at.elapsed().as_secs_f64() * 1000.0
}

fn median(mut values: Vec<f64>) -> f64 {
    values.sort_by(f64::total_cmp);
    values.get(values.len() / 2).copied().unwrap_or_default()
}

impl TypingFrames {
    fn mark(&mut self, mode: &'static str) {
        if mode != self.mode {
            self.report();
            self.mode = mode;
        }
        self.edited.get_or_insert_with(Instant::now);
    }

    fn rendered(&mut self, started: Instant, passes: f64) {
        let Some(edited) = self.edited else {
            return;
        };
        let waited = started.saturating_duration_since(edited).as_secs_f64() * 1000.0;
        self.pending.wait.get_or_insert(waited);
        self.pending.render += ms_since(started);
        self.pending.passes += passes;
    }

    fn rows(&mut self, started: Instant) {
        if self.edited.is_some() {
            self.pending.rows += ms_since(started);
        }
    }

    fn painted(&mut self) {
        if let Some(edited) = self.edited.take() {
            let phases = std::mem::take(&mut self.pending);
            self.intervals.push((ms_since(edited), phases));
        }
    }

    fn report(&mut self) {
        let samples = std::mem::take(&mut self.intervals);
        let Some(worst) = samples.iter().map(|(total, _)| *total).reduce(f64::max) else {
            return;
        };
        let of = |phase: fn(&(f64, Phases)) -> f64| median(samples.iter().map(phase).collect());
        let wait = |(_, phases): &(f64, Phases)| phases.wait.unwrap_or_default();
        let rest = |(total, phases): &(f64, Phases)| {
            total - phases.wait.unwrap_or_default() - phases.render - phases.rows
        };
        eprintln!(
            "desk: editor typing {} frames, median {:.2} ms, worst {worst:.2} ms{}; median wait {:.2}, render {:.2} (full-note passes {:.2}), rows {:.2}, layout and paint {:.2} ms",
            samples.len(),
            of(|(total, _)| *total),
            self.mode,
            of(wait),
            of(|(_, phases)| phases.render),
            of(|(_, phases)| phases.passes),
            of(|(_, phases)| phases.rows),
            of(rest),
        );
    }
}

impl CodeEditor {
    pub fn new(buffer: Buffer, syntax: Syntax, cx: &mut App) -> Self {
        Self::shown(buffer, Highlight::Tree(syntax), FileKind::Text, cx)
    }

    fn shown(buffer: Buffer, syntax: Highlight, kind: FileKind, cx: &mut App) -> Self {
        let mut editor = CodeEditor {
            buffer,
            syntax,
            kind,
            carets: vec![Caret::at(0)],
            marked: None,
            widest: 0,
            dragging: false,
            failure: None,
            marks: Rc::default(),
            scroll: UniformListScrollHandle::new(),
            bounds: Rc::default(),
            focus: cx.focus_handle(),
            on_save: None,
            since: Instant::now(),
            blink: None,
            bar: None,
            query: String::new(),
            found: Vec::new(),
            current: 0,
            edited_since_found: false,
            wrapping: false,
            wrap: Rc::default(),
            wrap_stale: Stale::Whole,
            wrap_reveal: Vec::new(),
            gutter: true,
            live: false,
            parsed: markdown::Parsed::default(),
            heights: Heights::default(),
            heights_stale: Stale::Whole,
            colour_stale: false,
            frames: std::env::var_os("DESK_FRAME_LOG").map(|_| Rc::default()),
            frames_flush: None,
            folds: BTreeMap::new(),
            shown_lines: None,
            chevron: None,
            folded_at: Rc::default(),
            keymap: Rc::default(),
            comment: None,
            vim: None,
            language: None,
            symbols: None,
            path: None,
            outlined: Outlined::Stale,
            link: None,
            on_jump: None,
            guides: false,
            active_guide: None,
            paints: std::env::var_os("DESK_FRAME_LOG").map(|_| Rc::default()),
            rest: None,
            wrote: None,
        };
        editor.widest = editor.widest_line();
        editor
    }

    pub fn open(path: &Path, cx: &mut App) -> Result<Self, String> {
        let started = Instant::now();
        let mut editor = Self::read(path, cx)?;
        open_phase("read", started);
        if editor.kind != FileKind::Text {
            return Ok(editor);
        }
        let started = Instant::now();
        let rope = editor.buffer.rope();
        let whole = rope.len_lines() <= FIRST_SCREEN_LINES;
        let first = match whole {
            true => None,
            false => Some(Buffer::from_text(
                &rope
                    .slice(..rope.line_to_char(FIRST_SCREEN_LINES))
                    .to_string(),
            )),
        };
        if let Some(syntax) = Syntax::for_path(path, first.as_ref().unwrap_or(&editor.buffer)) {
            let syntax = syntax.map_err(|error| error.to_string())?;
            eprintln!("desk: syntax {} {}", path.display(), syntax.report());
            editor.syntax = Highlight::Tree(syntax);
            if !whole {
                editor.rest = Some(Rest {
                    path: path.to_path_buf(),
                    _colouring: None,
                });
            }
        }
        open_phase("highlight", started);
        editor.wrapping = prose(path);
        editor.language = Language::from_path(path);
        editor.path = Some(Arc::from(path));
        let extension = path.extension().and_then(|extension| extension.to_str());
        editor.comment = LINE_COMMENTS
            .iter()
            .find(|(known, _)| extension.is_some_and(|ext| ext.eq_ignore_ascii_case(known)))
            .map(|(_, token)| *token);
        Ok(editor)
    }

    pub fn colour_rest(&mut self, cx: &mut Context<Self>) {
        let Some(rest) = &mut self.rest else {
            return;
        };
        let (path, rope) = (rest.path.clone(), self.buffer.rope().clone());
        let built = cx.background_executor().spawn(async move {
            let started = Instant::now();
            let syntax = Syntax::for_path(&path, &Buffer::from_text(&rope.to_string()));
            open_phase("highlight rest", started);
            syntax
        });
        rest._colouring = Some(cx.spawn(async move |this, cx| {
            let syntax = built.await;
            let applied = this.update(cx, |editor, cx| {
                editor.rest = None;
                match syntax {
                    Some(Ok(syntax)) => editor.syntax = Highlight::Tree(syntax),
                    Some(Err(error)) => editor.report("Could not colour", Err(error)),
                    None => {}
                }
                editor.refold();
                cx.notify();
            });
            if let Err(error) = applied {
                eprintln!("desk: editor is gone before its colours: {error}");
            }
        }));
    }

    pub fn plain(path: &Path, cx: &mut App) -> Result<Self, String> {
        let mut editor = Self::read(path, cx)?;
        editor.wrapping = true;
        Ok(editor)
    }

    fn read(path: &Path, cx: &mut App) -> Result<Self, String> {
        let shown = path.display();
        let size = fs::metadata(path)
            .map_err(|error| format!("{shown}: {error}"))?
            .len();
        if size > REFUSED_BYTES {
            return Err(format!(
                "{shown} is {} MB, over the {} MB the editor opens",
                size / READ_ONLY_BYTES,
                REFUSED_BYTES / READ_ONLY_BYTES
            ));
        }
        let bytes = fs::read(path).map_err(|error| format!("{shown}: {error}"))?;
        if binary(bytes.get(..SNIFF_BYTES).unwrap_or(&bytes)) {
            let note = format!("Binary file, {size} bytes, not shown.");
            let buffer = Buffer::from_text(&note);
            return Ok(Self::shown(
                buffer,
                Highlight::Plain,
                FileKind::Binary(size),
                cx,
            ));
        }
        let buffer = Buffer::from_text(&String::from_utf8_lossy(&bytes));
        if size > READ_ONLY_BYTES {
            return Ok(Self::shown(
                buffer,
                Highlight::Plain,
                FileKind::Large(size),
                cx,
            ));
        }
        Ok(Self::shown(buffer, Highlight::Plain, FileKind::Text, cx))
    }

    pub fn kind(&self) -> FileKind {
        self.kind
    }

    fn read_only(&self) -> bool {
        self.kind != FileKind::Text
    }

    pub fn on_save(
        mut self,
        save: impl Fn(&mut Buffer, &mut Window, &mut App) -> Result<(), String> + 'static,
    ) -> Self {
        self.on_save = Some(Rc::new(save));
        self
    }

    pub fn keymap(mut self, keymap: Rc<Keymap>) -> Self {
        self.keymap = keymap;
        self
    }

    pub fn vim(mut self, on: bool) -> Self {
        self.vim = on.then(Vim::default);
        self
    }

    pub fn guides(mut self, on: bool) -> Self {
        self.guides = on;
        self
    }

    pub fn on_jump(mut self, jump: impl Fn(Jump, &mut Window, &mut App) + 'static) -> Self {
        self.on_jump = Some(Rc::new(jump));
        self
    }

    pub fn set_symbols(&mut self, symbols: Option<Arc<Index>>) {
        self.symbols = symbols;
        self.link = None;
    }

    pub fn jump(&mut self, row: usize, col: usize, cx: &mut Context<Self>) {
        self.carets = vec![Caret::at(self.at_place((row, col)))];
        self.vim_clamp();
        self.reveal();
        self.touch(cx);
    }

    pub fn select(&mut self, ranges: &[Range<usize>], cx: &mut Context<Self>) {
        let end = self.buffer.len_chars();
        self.carets = ranges
            .iter()
            .map(|range| Caret {
                anchor: self.snap(range.start.min(end)),
                head: self.snap(range.end.min(end)),
                goal: None,
            })
            .collect();
        self.merge();
        self.reveal();
        self.touch(cx);
    }

    pub fn edit(
        &mut self,
        range: Range<usize>,
        text: &str,
        cx: &mut Context<Self>,
    ) -> Result<(), String> {
        let len = self.buffer.len_chars();
        if self.read_only() {
            return Err("the file is read only".to_owned());
        }
        if range.start > range.end || range.end > len {
            return Err(format!(
                "{range:?} is outside the {len} characters of the file"
            ));
        }
        self.marked = None;
        self.apply(&[range], text);
        self.reveal();
        self.touch(cx);
        Ok(())
    }

    pub fn save(&mut self, path: &Path, cx: &mut Context<Self>) -> Result<(), String> {
        if self.read_only() {
            return Err("the file is read only".to_owned());
        }
        self.buffer.save(path).map_err(|error| error.to_string())?;
        cx.notify();
        Ok(())
    }

    pub fn follow(
        &mut self,
        before: &Entity<CodeEditor>,
        path: String,
        seen: Instant,
        cx: &mut Context<Self>,
    ) {
        let old = before.read(cx);
        let caret = old.carets.last().and_then(|caret| old.place(caret.head));
        let lines = self.changed_since(old);
        let at_end = old.at_end(caret.map(|(line, _)| line));
        self.scroll = old.scroll.clone();
        self.focus = old.focus.clone();
        let last = self.buffer.line_count().saturating_sub(1);
        if let Some((line, column)) = caret {
            self.carets = vec![Caret::at(self.at_place((line.min(last), column)))];
            self.vim_clamp();
        }
        if at_end {
            reveal_row(&self.scroll, &self.heights, self.row_of(last));
        }
        let fade = cx.spawn(async move |this, cx| {
            cx.background_executor().timer(STRIPE_SOLID).await;
            if let Err(error) = this.update(cx, |_, cx| cx.notify()) {
                eprintln!("desk: editor is gone before its stripe fades: {error}");
            }
        });
        let logged = format!("desk: editor reloaded {path} {} lines changed", lines.len());
        self.wrote = Some(Wrote {
            lines,
            at: Instant::now(),
            unlogged: Some((logged, seen)),
            _fade: fade,
        });
    }

    fn at_end(&self, caret_line: Option<usize>) -> bool {
        let last = self.buffer.line_count().saturating_sub(1);
        let offset = self.scroll.0.borrow().base_handle.offset().y;
        let seen = -offset + self.bounds.get().size.height;
        caret_line == Some(last) || seen > self.heights.top(self.row_of(last))
    }

    fn changed_since(&self, before: &CodeEditor) -> Range<usize> {
        let (old, new) = (before.buffer.rope(), self.buffer.rope());
        let same = |a: usize, b: usize| match (old.get_line(a), new.get_line(b)) {
            (Some(a), Some(b)) => bare(a.chars()).eq(bare(b.chars())),
            _ => false,
        };
        let (old_lines, new_lines) = (old.len_lines(), new.len_lines());
        let shared = old_lines.min(new_lines);
        let top = (0..shared).take_while(|&line| same(line, line)).count();
        let bottom = (1..=shared - top)
            .take_while(|&back| same(old_lines - back, new_lines - back))
            .count();
        top..new_lines - bottom
    }

    fn stripe(&mut self, window: &mut Window) -> Option<Stripe> {
        let wrote = self.wrote.as_ref()?;
        let fading = wrote.at.elapsed().saturating_sub(STRIPE_SOLID);
        let faded = fading.as_secs_f32() / STRIPE_FADE.as_secs_f32();
        if faded >= 1.0 {
            self.wrote = None;
            return None;
        }
        if faded > 0.0 {
            window.request_animation_frame();
        }
        Some(Stripe {
            lines: wrote.lines.clone(),
            token: STRIPE,
            opacity: 1.0 - EASE_OUT(faded),
        })
    }

    fn word_at(&self, at: usize) -> Option<Range<usize>> {
        let word = |at: usize| self.char_at(at).is_some_and(|ch| class(ch) == Class::Word);
        let inside = match word(at) {
            true => at,
            false => at.checked_sub(1).filter(|before| word(*before))?,
        };
        Some(self.word_left(inside + 1)..self.word_right(inside))
    }

    fn word(&self, range: Range<usize>) -> Option<String> {
        Some(self.buffer.rope().get_slice(range)?.to_string())
    }

    fn outline_later(&mut self, cx: &mut Context<Self>) {
        let Some(path) = self.path.clone() else {
            return;
        };
        let rope = self.buffer.rope().clone();
        let outlining = cx.spawn(async move |this, cx| {
            cx.background_executor().timer(OUTLINE_SETTLE).await;
            let built = cx
                .background_executor()
                .spawn(async move { Outline::build(&path, &rope.to_string()) })
                .await;
            let outline = built.unwrap_or_else(|error| {
                eprintln!("desk: editor could not outline: {error}");
                Outline::default()
            });
            if let Err(error) = this.update(cx, |editor, _| {
                editor.outlined = Outlined::Built(outline);
            }) {
                eprintln!("desk: editor is gone before its outline: {error}");
            }
        });
        self.outlined = Outlined::Building {
            _outlining: outlining,
        };
    }

    fn outline(&mut self) -> Option<&Outline> {
        if !matches!(self.outlined, Outlined::Built(_)) {
            let path = self.path.clone()?;
            let built = Outline::build(&path, &self.buffer.text()).unwrap_or_else(|error| {
                eprintln!("desk: editor could not outline: {error}");
                Outline::default()
            });
            self.outlined = Outlined::Built(built);
        }
        match &self.outlined {
            Outlined::Built(outline) => Some(outline),
            Outlined::Stale | Outlined::Building { .. } => None,
        }
    }

    fn named(&mut self, range: Range<usize>) -> Option<(String, (usize, usize), Vec<Symbol>)> {
        self.language?;
        let place = self.place(range.start)?;
        let name = self.word(range)?;
        let here = self.outline()?.definitions(&name, place);
        Some((name, place, here))
    }

    fn defined(&mut self, range: Range<usize>) -> bool {
        let Some((name, _, here)) = self.named(range) else {
            return false;
        };
        let elsewhere = |index: &Index| {
            self.language
                .is_some_and(|language| index.definitions(&name, language).next().is_some())
        };
        !here.is_empty() || self.symbols.as_deref().is_some_and(elsewhere)
    }

    fn definition_jump(&mut self, at: usize, point: Point<Pixels>, since: Instant) -> Option<Jump> {
        let range = self.word_at(at)?;
        let (name, (row, col), here) = self.named(range)?;
        Some(Jump::Definition(Asked {
            name,
            at: point,
            row,
            col,
            here,
            since,
        }))
    }

    fn keyed_jump(&mut self, keys: &Keystroke, window: &Window, cx: &App) -> Option<Jump> {
        self.on_jump.as_ref()?;
        let held = keys.modifiers;
        if held.control || held.shift || held.platform {
            return None;
        }
        let head = self.carets.last()?.head;
        let (row, col) = self.place(head)?;
        let way = match (keys.key.as_str(), held.alt) {
            ("left", true) => Way::Back,
            ("right", true) => Way::Forward,
            ("f12", false) => {
                let since = Instant::now();
                let (shown, x) = self.x_of(head, window, cx)?;
                let origin = code_origin(self.bounds.get(), &self.scroll, &self.heights, shown);
                let below = point(origin.x + x, origin.y + self.heights.height(shown));
                return self.definition_jump(head, below, since);
            }
            _ => return None,
        };
        Some(Jump::Travel { way, row, col })
    }

    fn point_link(
        &mut self,
        position: Point<Pixels>,
        held: bool,
        window: &Window,
        cx: &mut Context<Self>,
    ) {
        let range = (held && self.on_jump.is_some() && self.in_text(position))
            .then(|| self.char_at_point(position, window, cx))
            .flatten()
            .and_then(|at| self.word_at(at));
        let link = range.filter(|range| self.defined(range.clone()));
        if link != self.link {
            self.link = link;
            cx.notify();
        }
    }

    fn in_text(&self, position: Point<Pixels>) -> bool {
        let area = self.bounds.get();
        area.contains(&position) && position.x >= area.left() + px(NUMBER_COLUMN)
    }

    fn modifiers_changed(
        &mut self,
        event: &ModifiersChangedEvent,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let held = event.modifiers.secondary();
        self.point_link(window.mouse_position(), held, window, cx);
    }

    fn leap(&self, jump: Jump, window: &mut Window, cx: &mut Context<Self>) {
        let Some(on_jump) = self.on_jump.clone() else {
            return;
        };
        window.defer(cx, move |window, cx| on_jump(jump, window, cx));
    }

    pub fn marks(&self) -> &Marks {
        &self.marks
    }

    pub fn set_marks(&mut self, marks: Marks) {
        self.marks = Rc::new(marks);
    }

    pub fn buffer(&self) -> &Buffer {
        &self.buffer
    }

    pub fn syntax(&self) -> &Highlight {
        &self.syntax
    }

    pub fn carets(&self) -> impl Iterator<Item = (usize, usize)> + '_ {
        self.carets.iter().map(|caret| (caret.anchor, caret.head))
    }

    pub fn failure(&self) -> Option<(&'static str, &SharedString)> {
        self.failure.as_ref().map(|(what, error)| (*what, error))
    }

    pub fn finding(&self) -> Option<(&str, usize, usize)> {
        let shown = self.current + usize::from(!self.found.is_empty());
        (!self.query.is_empty()).then_some((self.query.as_str(), shown, self.found.len()))
    }

    pub fn find(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let bar = self
            .bar
            .get_or_insert_with(|| {
                let bar = FindBar::new(window, cx);
                let changed = cx.listener(|editor, (query, at): &(String, usize), _, cx| {
                    editor.found(query, *at, cx);
                });
                let closed = cx.listener(|editor, _: &(), _, cx| editor.unfound(cx));
                let replaced = cx.listener(|editor, (what, with): &(Replace, String), _, cx| {
                    editor.replace(*what, with, cx);
                });
                bar.update(cx, |bar, _| {
                    bar.on_change(move |query, at, window, cx| {
                        changed(&(query.to_owned(), at), window, cx);
                    });
                    bar.on_close(move |window, cx| closed(&(), window, cx));
                    bar.on_replace(move |what, with, window, cx| {
                        replaced(&(what, with.to_owned()), window, cx);
                    });
                });
                bar
            })
            .clone();
        bar.update(cx, |bar, cx| bar.open(window, cx));
        cx.notify();
    }

    fn search(&mut self, cx: &mut Context<Self>) {
        let matching = self
            .bar
            .as_ref()
            .map(|bar| bar.read(cx).matching())
            .unwrap_or_default();
        self.found = self.char_ranges(&self.query, matching);
        if self.current >= self.found.len() {
            self.current = 0;
        }
        self.edited_since_found = false;
        let total = self.found.len();
        if let Some(bar) = &self.bar {
            bar.update(cx, |bar, cx| bar.set_total(total, cx));
        }
    }

    fn found(&mut self, query: &str, at: usize, cx: &mut Context<Self>) {
        query.clone_into(&mut self.query);
        self.current = at;
        self.search(cx);
        if let Some(found) = self.found.get(self.current) {
            self.carets = vec![Caret {
                anchor: found.start,
                head: found.end,
                goal: None,
            }];
            self.reveal();
        }
        cx.notify();
    }

    fn unfound(&mut self, cx: &mut Context<Self>) {
        self.query.clear();
        self.found.clear();
        self.current = 0;
        cx.notify();
    }

    fn char_ranges(&self, query: &str, matching: Matching) -> Vec<Range<usize>> {
        let text = self.buffer.text();
        let rope = self.buffer.rope();
        find_matching(&text, query, matching)
            .into_iter()
            .filter_map(|bytes| {
                Some(
                    rope.try_byte_to_char(bytes.start).ok()?
                        ..rope.try_byte_to_char(bytes.end).ok()?,
                )
            })
            .collect()
    }

    fn replace(&mut self, what: Replace, with: &str, cx: &mut Context<Self>) {
        let started = Instant::now();
        let targets = match what {
            Replace::Current => self.found.get(self.current).cloned().into_iter().collect(),
            Replace::All => self.found.clone(),
        };
        self.apply(&targets, with);
        self.search(cx);
        if what == Replace::All {
            let ms = started.elapsed().as_secs_f64() * 1000.0;
            eprintln!(
                "desk: editor replace {} matches in {ms:.2} ms",
                targets.len()
            );
        }
        if let Some(found) = self.found.get(self.current) {
            self.carets = vec![Caret {
                anchor: found.start,
                head: found.end,
                goal: None,
            }];
        }
        self.reveal();
        self.touch(cx);
    }

    fn add_next_match(&mut self) {
        let Some(last) = self.carets.last().map(|caret| caret.range()) else {
            return;
        };
        if last.is_empty() {
            self.carets = self
                .carets
                .iter()
                .map(|caret| match self.word_at(caret.head) {
                    Some(word) if caret.range().is_empty() => Caret {
                        anchor: word.start,
                        head: word.end,
                        goal: None,
                    },
                    _ => *caret,
                })
                .collect();
            return self.merge();
        }
        let Some(needle) = self.word(last.clone()) else {
            return;
        };
        let matching = Matching {
            case: true,
            whole: self.word_at(last.start) == Some(last.clone()),
        };
        let taken = self.selections();
        let free: Vec<Range<usize>> = self
            .char_ranges(&needle, matching)
            .into_iter()
            .filter(|hit| !taken.contains(hit))
            .collect();
        let next = free
            .iter()
            .find(|hit| hit.start >= last.end)
            .or(free.first());
        if let Some(hit) = next {
            self.carets.push(Caret {
                anchor: hit.start,
                head: hit.end,
                goal: None,
            });
            self.merge();
        }
    }

    fn char_at(&self, at: usize) -> Option<char> {
        self.buffer.rope().get_char(at)
    }

    fn before(&self, at: usize) -> usize {
        let crlf =
            at >= 2 && self.char_at(at - 1) == Some('\n') && self.char_at(at - 2) == Some('\r');
        if crlf { at - 2 } else { at.saturating_sub(1) }
    }

    fn after(&self, at: usize) -> usize {
        let crlf = self.char_at(at) == Some('\r') && self.char_at(at + 1) == Some('\n');
        (at + if crlf { 2 } else { 1 }).min(self.buffer.len_chars())
    }

    fn snap(&self, at: usize) -> usize {
        let at = at.min(self.buffer.len_chars());
        let inside = at > 0 && self.char_at(at - 1) == Some('\r') && self.char_at(at) == Some('\n');
        if inside { at - 1 } else { at }
    }

    fn word_right(&self, mut at: usize) -> usize {
        while self.char_at(at).is_some_and(char::is_whitespace) {
            at += 1;
        }
        let kind = self.char_at(at).map(class);
        while kind.is_some() && self.char_at(at).map(class) == kind {
            at += 1;
        }
        at
    }

    fn word_left(&self, mut at: usize) -> usize {
        while at > 0 && self.char_at(at - 1).is_some_and(char::is_whitespace) {
            at -= 1;
        }
        let kind = at
            .checked_sub(1)
            .and_then(|before| self.char_at(before))
            .map(class);
        while at > 0 && kind.is_some() && self.char_at(at - 1).map(class) == kind {
            at -= 1;
        }
        at
    }

    fn line_len(&self, row: usize) -> usize {
        self.buffer.line(row).map_or(0, |line| line.chars().count())
    }

    fn widest_line(&self) -> usize {
        let rope = self.buffer.rope();
        (0..self.buffer.line_count())
            .max_by_key(|row| rope.get_line(*row).map_or(0, |line| line.len_chars()))
            .unwrap_or(0)
    }

    fn drawn(&self, line: usize) -> Drawn {
        let full = self.buffer.line(line).unwrap_or_default();
        let text = cut(&full, LONG_LINE_BYTES);
        if !self.live {
            return Drawn::new(text, &[]);
        }
        let found = self.parsed.line(text);
        let hidden = match self.revealed(line) {
            true => &[][..],
            false => &found.markup,
        };
        Drawn {
            heading: found.heading,
            ..Drawn::new(text, hidden)
        }
    }

    fn head_lines(&self) -> Vec<usize> {
        self.carets
            .iter()
            .filter_map(|caret| self.buffer.char_to_line(caret.head).ok())
            .collect()
    }

    fn revealed(&self, line: usize) -> bool {
        self.carets
            .iter()
            .any(|caret| self.buffer.char_to_line(caret.head).ok() == Some(line))
    }

    fn live_lines(&self, rows: Range<usize>) -> Vec<(CodeLine, Drawn)> {
        rows.map(|row| {
            let full = self.buffer.line(row).unwrap_or_default();
            let text = cut(&full, LONG_LINE_BYTES);
            let found = self.parsed.line(text);
            let revealed = self.revealed(row);
            let hidden = match revealed {
                true => &[][..],
                false => &found.markup,
            };
            let drawn = Drawn {
                heading: found.heading,
                ..Drawn::new(text, hidden)
            };
            let bytes = |range: &Range<usize>| drawn.byte(range.start)..drawn.byte(range.end);
            let markup = found
                .markup
                .iter()
                .filter(|_| revealed)
                .map(|range| (bytes(range), markdown::MARKUP));
            let runs = found
                .runs
                .iter()
                .map(|(range, token)| (bytes(range), *token))
                .chain(markup)
                .filter(|(range, _)| !range.is_empty())
                .collect();
            let line = CodeLine {
                text: drawn.text.clone().into(),
                runs,
                heading: found.heading,
                faces: found
                    .faces
                    .iter()
                    .map(|(range, face)| (bytes(range), *face))
                    .collect(),
                ..CodeLine::default()
            };
            (line, drawn)
        })
        .collect()
    }

    pub fn set_gutter(&mut self, shown: bool) {
        self.gutter = shown;
        self.wrap_stale = Stale::Whole;
    }

    pub fn set_live(&mut self, on: bool, cx: &mut Context<Self>) {
        self.live = on;
        self.wrap_stale = Stale::Whole;
        self.heights_stale = Stale::Whole;
        if on || !std::mem::take(&mut self.colour_stale) {
            return;
        }
        match self.path.clone() {
            Some(path) if matches!(self.syntax, Highlight::Tree(_)) || self.rest.is_some() => {
                self.buffer.take_applied();
                self.rest = Some(Rest {
                    path: path.to_path_buf(),
                    _colouring: None,
                });
                self.colour_rest(cx);
            }
            _ => self.colour_edits(),
        }
    }

    fn remeasure(&mut self) {
        let stale = std::mem::replace(&mut self.heights_stale, Stale::Fresh);
        if stale == Stale::Fresh {
            return;
        }
        if !self.live {
            self.heights = Heights::default();
            return;
        }
        self.parsed.trim(self.buffer.line_count().saturating_mul(2));
        let spliced = match stale {
            Stale::Lines { at, old, new } if self.shown_lines.is_none() => {
                let heading = |row: usize| {
                    let line = self.wrapped().map_or(row, |wrap| wrap.locate(row).0);
                    self.heading(line)
                };
                self.heights
                    .spliced(at..at + old, (at..at + new).map(heading))
            }
            Stale::Fresh | Stale::Lines { .. } | Stale::Whole => None,
        };
        self.heights = spliced.unwrap_or_else(|| self.measured());
    }

    fn heading(&self, line: usize) -> Heading {
        self.buffer
            .rope()
            .get_line(line)
            .map_or(Heading::Body, |text| markdown::heading(text.chars()))
    }

    fn measured(&self) -> Heights {
        match self.wrapped() {
            Some(wrap) => Heights::measured(
                (0..self.buffer.line_count())
                    .flat_map(|line| std::iter::repeat_n(self.heading(line), wrap.pieces(line))),
            ),
            None => {
                Heights::measured((0..self.row_count()).map(|row| self.heading(self.line_of(row))))
            }
        }
    }

    pub fn live(&self) -> bool {
        self.live
    }

    fn wrapped(&self) -> Option<&Wrap> {
        (self.wrapping && self.wrap.rows() > 0).then_some(&*self.wrap)
    }

    fn rewrap(&mut self, cx: &App) {
        let width = wrap_width(self.bounds.get());
        if !self.wrapping || width <= px(0.0) {
            return;
        }
        let reveal = match self.live {
            true => self.head_lines(),
            false => Vec::new(),
        };
        let theme = ActiveTheme::theme(cx);
        let kept = width == self.wrap.width() && self.wrap.rows() > 0;
        let stale = match std::mem::replace(&mut self.wrap_stale, Stale::Fresh) {
            Stale::Fresh if kept => Stale::Lines {
                at: 0,
                old: 0,
                new: 0,
            },
            stale => stale,
        };
        if let (Stale::Lines { at, old, new }, true) = (stale, kept) {
            let moved = |line: usize| match line {
                line if line < at => Some(line),
                line if line >= at + old => Some(line - old + new),
                _ => None,
            };
            let before: Vec<usize> = self.wrap_reveal.iter().filter_map(|l| moved(*l)).collect();
            self.wrap_reveal = reveal;
            if old + new > 0 {
                self.respan(at..at + old, new, &theme, cx);
            }
            let mut touched: Vec<usize> = before
                .iter()
                .chain(&self.wrap_reveal)
                .filter(|line| !(at..at + new).contains(line))
                .filter(|line| before.contains(line) != self.wrap_reveal.contains(line))
                .copied()
                .collect();
            touched.sort_unstable();
            touched.dedup();
            for line in touched {
                self.respan(line..line + 1, 1, &theme, cx);
            }
            return;
        }
        self.wrap_reveal = reveal;
        let started = Instant::now();
        let first = self.wrap.rows() == 0;
        let keys = (0..self.buffer.line_count()).map(|line| self.wrap_key(line));
        let text_of = |line: usize| {
            let drawn = self.drawn(line);
            (drawn.text, drawn.heading)
        };
        self.wrap = Rc::new(Wrap::new(&self.wrap, keys, text_of, width, &theme, cx));
        self.heights_stale = Stale::Whole;
        if first {
            open_phase("wrap", started);
        }
    }

    fn respan(&mut self, old: Range<usize>, new: usize, theme: &Theme, cx: &App) {
        let fresh = (old.start..old.start + new)
            .map(|line| {
                let drawn = self.drawn(line);
                (self.wrap_key(line), drawn.text, drawn.heading)
            })
            .collect();
        let (gone, came) = Rc::make_mut(&mut self.wrap).splice(old, fresh, theme, cx);
        self.heights_stale = self.heights_stale.then(Stale::Lines {
            at: gone.start,
            old: gone.len(),
            new: came.len(),
        });
    }

    fn wrap_key(&self, line: usize) -> u64 {
        let mut hasher = DefaultHasher::new();
        let text = self.buffer.rope().get_line(line);
        for chunk in text.iter().flat_map(|text| text.chunks()) {
            hasher.write(chunk.as_bytes());
        }
        let revealed = self.live && self.wrap_reveal.contains(&line);
        hasher.write_u8(u8::from(self.live) << 1 | u8::from(revealed));
        hasher.finish()
    }

    fn toggle_wrap(&mut self) {
        self.wrapping = !self.wrapping;
        self.wrap_stale = Stale::Whole;
        self.heights_stale = Stale::Whole;
        for caret in &mut self.carets {
            caret.goal = None;
        }
        let base = self.scroll.0.borrow().base_handle.clone();
        base.set_offset(point(px(0.0), base.offset().y));
        let state = if self.wrapping { "on" } else { "off" };
        eprintln!("desk: editor wrap {state}");
        self.refold();
    }

    fn foldable(&self, line: usize) -> Option<usize> {
        match &self.syntax {
            Highlight::Tree(syntax) if !self.wrapping => syntax.fold(line),
            Highlight::Tree(_) | Highlight::Plain => None,
        }
    }

    fn refold(&mut self) {
        let folds = std::mem::take(&mut self.folds);
        self.folds = folds
            .into_keys()
            .filter_map(|header| Some((header, self.foldable(header)?)))
            .collect();
        if self.shown_lines.is_some() || !self.folds.is_empty() {
            self.heights_stale = Stale::Whole;
        }
        if self.folds.is_empty() {
            self.shown_lines = None;
            return;
        }
        let mut shown = Vec::with_capacity(self.buffer.line_count());
        let mut next = 0;
        for (&header, &last) in &self.folds {
            if header < next {
                continue;
            }
            shown.extend(next..=header);
            next = last + 1;
        }
        shown.extend(next..self.buffer.line_count());
        self.shown_lines = Some(shown.into());
    }

    fn fold(&mut self, header: usize) {
        let Some(last) = self.foldable(header) else {
            return;
        };
        self.folded_at.set(Some(Instant::now()));
        self.folds.insert(header, last);
        let Ok(start) = self.buffer.line_to_char(header) else {
            return;
        };
        let end = start + self.line_len(header);
        let hidden = header + 1..last + 1;
        for caret in &mut self.carets {
            let line = self.buffer.char_to_line(caret.head);
            if line.is_ok_and(|line| hidden.contains(&line)) {
                *caret = Caret::at(end);
            }
        }
        self.merge();
        self.refold();
    }

    fn unfold(&mut self, header: usize) {
        if self.folds.remove(&header).is_some() {
            self.folded_at.set(Some(Instant::now()));
            self.refold();
        }
    }

    fn toggle_fold(&mut self, header: usize) {
        match self.folds.contains_key(&header) {
            true => self.unfold(header),
            false => self.fold(header),
        }
    }

    fn caret_line(&self) -> Option<usize> {
        let caret = self.carets.last()?;
        self.buffer.char_to_line(caret.head).ok()
    }

    fn indent_of(&self, line: usize) -> Option<usize> {
        indent_levels(self.buffer.rope().get_line(line)?.chars())
    }

    fn levels(&self, line: usize) -> usize {
        if let Some(levels) = self.indent_of(line) {
            return levels;
        }
        let end = self.buffer.line_count().min(line + 1 + GUIDE_REACH);
        let above = (line.saturating_sub(GUIDE_REACH)..line)
            .rev()
            .find_map(|line| self.indent_of(line));
        let below = (line + 1..end).find_map(|line| self.indent_of(line));
        above.unwrap_or(0).min(below.unwrap_or(0))
    }

    fn guide_block(&self) -> Option<(usize, Range<usize>)> {
        let line = self.caret_line()?;
        let here = self.levels(line);
        let header = self.levels(line + 1) > here;
        let guide = match header {
            true => here,
            false => here.checked_sub(1)?,
        };
        let start = line + usize::from(header);
        let inside = |line: usize| self.indent_of(line).map(|levels| (line, levels > guide));
        let edge = |found: Option<(usize, bool)>| found.map_or(start, |(line, _)| line);
        let first = (start.saturating_sub(GUIDE_REACH)..start)
            .rev()
            .filter_map(inside)
            .take_while(|(_, within)| *within)
            .last();
        let end = self.buffer.line_count().min(start + 1 + GUIDE_REACH);
        let last = (start + 1..end)
            .filter_map(inside)
            .take_while(|(_, within)| *within)
            .last();
        Some((guide, edge(first)..edge(last) + 1))
    }

    fn fold_at_caret(&mut self) {
        let header = match (&self.syntax, self.caret_line()) {
            (Highlight::Tree(syntax), Some(line)) => syntax.fold_header(line),
            _ => None,
        };
        if let Some(header) = header {
            self.fold(header);
        }
    }

    fn unfold_carets(&mut self) {
        let lines: Vec<usize> = self
            .carets
            .iter()
            .filter_map(|caret| self.buffer.char_to_line(caret.head).ok())
            .collect();
        let before = self.folds.len();
        self.folds
            .retain(|header, last| !lines.iter().any(|line| header < line && line <= last));
        if self.folds.len() != before {
            self.refold();
        }
    }

    fn row_count(&self) -> usize {
        self.shown_lines
            .as_ref()
            .map_or(self.buffer.line_count(), |shown| shown.len())
    }

    fn line_of(&self, row: usize) -> usize {
        let row = row.min(self.row_count().saturating_sub(1));
        match &self.shown_lines {
            Some(shown) => shown.get(row).copied().unwrap_or(0),
            None => row,
        }
    }

    fn row_of(&self, line: usize) -> usize {
        self.shown_lines
            .as_ref()
            .map_or(line, |shown| shown.partition_point(|at| *at < line))
    }

    fn hover(&mut self, position: Point<Pixels>, cx: &mut Context<Self>) {
        let (row, _) = code_place(self.bounds.get(), &self.scroll, &self.heights, position);
        let line = self.line_of(row);
        let chevron =
            Some(line).filter(|line| row < self.row_count() && self.foldable(*line).is_some());
        if chevron != self.chevron {
            self.chevron = chevron;
            cx.notify();
        }
    }

    fn gutter_fold(&mut self, position: Point<Pixels>) -> bool {
        let area = self.bounds.get();
        let x = position.x - area.left();
        if !self.gutter || x < px(NUMBER_COLUMN - NUMBER_PAD) || x >= px(NUMBER_COLUMN) {
            return false;
        }
        let (row, _) = code_place(area, &self.scroll, &self.heights, position);
        let line = self.line_of(row);
        let foldable = self.folds.contains_key(&line) || self.foldable(line).is_some();
        if row >= self.row_count() || !foldable {
            return false;
        }
        self.toggle_fold(line);
        true
    }

    fn visual(&self, at: usize, wrap: &Wrap) -> Option<(usize, usize)> {
        let line = self.buffer.char_to_line(at).ok()?;
        let drawn = self.drawn(line);
        let byte = drawn.byte(at - self.buffer.line_to_char(line).ok()?);
        let row = wrap.row_of(line, byte);
        let within = wrap.piece(line, wrap.locate(row).1, drawn.text.len());
        Some((row, drawn.column(byte) - drawn.column(within.start)))
    }

    fn visual_step(&self, caret: Caret, up: bool, wrap: &Wrap) -> Option<(usize, Option<usize>)> {
        let (row, column) = self.visual(caret.head, wrap)?;
        let goal = caret.goal.unwrap_or(column);
        let next = match up {
            true => row.checked_sub(1),
            false => Some(row + 1).filter(|next| *next < wrap.rows()),
        };
        let Some(next) = next else {
            let edge = if up { 0 } else { self.buffer.len_chars() };
            return Some((edge, Some(goal)));
        };
        let (line, piece) = wrap.locate(next);
        let drawn = self.drawn(line);
        let within = wrap.piece(line, piece, drawn.text.len());
        let before = drawn.column(within.start);
        let chars = drawn.column(within.end) - before;
        let most = match piece + 1 == wrap.pieces(line) {
            true => chars,
            false => chars.saturating_sub(1),
        };
        let start = self.buffer.line_to_char(line).ok()?;
        Some((start + before + goal.min(most), Some(goal)))
    }

    fn moved(&self, caret: Caret, key: &str, word: bool) -> Option<(usize, Option<usize>)> {
        let head = caret.head;
        let row = self.buffer.char_to_line(head).ok()?;
        let start = self.buffer.line_to_char(row).ok()?;
        if let ("up" | "down", Some(wrap)) = (key, self.wrapped()) {
            return self.visual_step(caret, key == "up", wrap);
        }
        Some(match (key, word) {
            ("left", false) => (self.before(head), None),
            ("right", false) => (self.after(head), None),
            ("left", true) => (self.word_left(head), None),
            ("right", true) => (self.word_right(head), None),
            ("home", false) => (start, None),
            ("end", false) => (start + self.line_len(row), None),
            ("home", true) => (0, None),
            ("end", true) => (self.buffer.len_chars(), None),
            ("up" | "down", _) => {
                let goal = caret.goal.unwrap_or(head - start);
                let shown = self.row_of(row);
                let next = match key {
                    "up" => shown.checked_sub(1),
                    _ => Some(shown + 1).filter(|next| *next < self.row_count()),
                }
                .map(|next| self.line_of(next));
                let to = match next {
                    Some(next) => {
                        self.buffer.line_to_char(next).ok()? + goal.min(self.line_len(next))
                    }
                    None if key == "up" => 0,
                    None => self.buffer.len_chars(),
                };
                (to, Some(goal))
            }
            _ => return None,
        })
    }

    fn merge(&mut self) {
        let mut kept: Vec<Caret> = Vec::with_capacity(self.carets.len());
        for caret in self.carets.drain(..).rev() {
            let range = caret.range();
            let clash = kept.iter().any(|other| {
                let taken = other.range();
                other.head == caret.head || (range.start < taken.end && taken.start < range.end)
            });
            if !clash {
                kept.push(caret);
            }
        }
        kept.reverse();
        self.carets = kept;
    }

    fn travel(&mut self, key: &str, word: bool, extend: bool) {
        let moved: Option<Vec<Caret>> = self
            .carets
            .iter()
            .map(|caret| {
                let range = caret.range();
                let collapse = !extend && !word && !range.is_empty();
                let (head, goal) = match key {
                    "left" if collapse => (range.start, None),
                    "right" if collapse => (range.end, None),
                    _ => self.moved(*caret, key, word)?,
                };
                let anchor = if extend { caret.anchor } else { head };
                Some(Caret { anchor, head, goal })
            })
            .collect();
        if let Some(moved) = moved {
            self.carets = moved;
            self.merge();
        }
    }

    fn stack(&mut self, key: &str) {
        let Some(last) = self.carets.last().copied() else {
            return;
        };
        let Some((head, goal)) = self.moved(last, key, false) else {
            return;
        };
        if self.buffer.char_to_line(head).ok() == self.buffer.char_to_line(last.head).ok() {
            return;
        }
        self.carets.push(Caret {
            goal,
            ..Caret::at(head)
        });
        self.merge();
    }

    fn report(&mut self, what: &'static str, result: Result<(), impl Display>) {
        if let Err(error) = result {
            self.failure = Some((what, error.to_string().into()));
        }
    }

    fn colour_edits(&mut self) {
        if let Some(rest) = self.rest.take() {
            self.buffer.take_applied();
            match Syntax::for_path(&rest.path, &self.buffer) {
                Some(Ok(syntax)) => self.syntax = Highlight::Tree(syntax),
                Some(Err(error)) => self.report("Could not colour", Err(error)),
                None => {}
            }
        } else if let Highlight::Tree(syntax) = &mut self.syntax {
            let synced = syntax.sync(&mut self.buffer);
            self.report("Could not colour", synced);
        }
    }

    fn settle(&mut self, lines: usize, edited: Stale) {
        self.wrote = None;
        self.wrap_stale = self.wrap_stale.then(edited);
        if self.wrapped().is_none() {
            self.heights_stale = self.heights_stale.then(edited);
        }
        self.link = None;
        self.outlined = Outlined::Stale;
        match self.live {
            true => self.colour_stale = true,
            false => self.colour_edits(),
        }
        self.edited_since_found = !self.query.is_empty();
        self.carets = self
            .buffer
            .selections()
            .iter()
            .map(|range| Caret {
                anchor: range.start,
                head: range.end,
                goal: None,
            })
            .collect();
        let rope = self.buffer.rope();
        let width = |row: &usize| rope.get_line(*row).map_or(0, |line| line.len_chars());
        self.widest = if self.buffer.line_count() == lines {
            self.carets
                .iter()
                .filter_map(|caret| self.buffer.char_to_line(caret.head).ok())
                .chain([self.widest])
                .max_by_key(width)
                .unwrap_or(self.widest)
        } else {
            self.widest_line()
        };
        self.refold();
    }

    fn apply(&mut self, ranges: &[Range<usize>], text: &str) {
        let ranges: Vec<Range<usize>> = ranges
            .iter()
            .filter(|range| !range.is_empty() || !text.is_empty())
            .cloned()
            .collect();
        if ranges.is_empty() || self.read_only() {
            return;
        }
        let lines = self.buffer.line_count();
        let edits = self.line_edits(&ranges, text);
        let rope = self.buffer.rope();
        let span = ranges
            .iter()
            .filter_map(|range| {
                let first = rope.try_char_to_line(range.start).ok()?;
                Some((first, rope.try_char_to_line(range.end).ok()?.max(first)))
            })
            .reduce(|(first, last), (at, end)| (first.min(at), last.max(end)));
        match self.buffer.edit(&ranges, text) {
            Ok(()) => {
                self.shift(&edits);
                let edited = span.map_or(Stale::Whole, |(at, last)| {
                    let old = last - at + 1;
                    Stale::Lines {
                        at,
                        old,
                        new: (old + self.buffer.line_count()).saturating_sub(lines),
                    }
                });
                self.settle(lines, edited);
                if let Some(frames) = &self.frames {
                    let mode = match self.live {
                        true => ", rendered",
                        false => "",
                    };
                    frames.borrow_mut().mark(mode);
                }
            }
            Err(error) => self.report("Could not edit", Err(error)),
        }
    }

    fn line_edits(&self, ranges: &[Range<usize>], text: &str) -> Vec<LineEdit> {
        if self.marks.is_empty() && self.folds.is_empty() {
            return Vec::new();
        }
        let mut ranges = ranges.to_vec();
        ranges.sort_by_key(|range| std::cmp::Reverse(range.start));
        let rope = self.buffer.rope();
        ranges
            .into_iter()
            .filter_map(|range| {
                let line = rope.try_char_to_line(range.start).ok()?;
                let removed = rope.get_slice(range.clone())?;
                let line_start = rope.try_line_to_char(line).ok() == Some(range.start);
                Some(LineEdit::new(line, line_start, &removed.to_string(), text))
            })
            .collect()
    }

    fn shift(&mut self, edits: &[LineEdit]) {
        for edit in edits {
            self.folds
                .retain(|header, last| !edit.touches(&(*header + 1..*last + 1)));
            self.folds = edit.follow(&self.folds);
        }
        if !self.marks.is_empty() {
            let marks = edits
                .iter()
                .fold((*self.marks).clone(), |marks, edit| edit.follow(&marks));
            self.marks = Rc::new(marks);
        }
    }

    fn selections(&self) -> Vec<Range<usize>> {
        self.carets.iter().map(|caret| caret.range()).collect()
    }

    fn insert(&mut self, text: &str) {
        self.marked = None;
        self.apply(&self.selections(), text);
    }

    fn erase(&mut self, back: bool) {
        let ranges: Vec<Range<usize>> = self
            .carets
            .iter()
            .map(|caret| match (caret.range(), back) {
                (range, _) if !range.is_empty() => range,
                (_, true) => self.before(caret.head)..caret.head,
                (_, false) => caret.head..self.after(caret.head),
            })
            .collect();
        self.apply(&ranges, "");
    }

    fn history(&mut self, back: bool) {
        if self.read_only() {
            return;
        }
        let lines = self.buffer.line_count();
        let before = self.buffer.rope().clone();
        let moved = if back {
            self.buffer.undo()
        } else {
            self.buffer.redo()
        };
        if !moved {
            return;
        }
        let after = self.buffer.rope();
        if !self.marks.is_empty() || !self.folds.is_empty() {
            let same = |pair: &(char, char)| pair.0 == pair.1;
            let start = before.chars().zip(after.chars()).take_while(same).count();
            let (old_len, new_len) = (before.len_chars(), after.len_chars());
            let tail = before
                .chars_at(old_len)
                .reversed()
                .zip(after.chars_at(new_len).reversed())
                .take_while(same)
                .count()
                .min(old_len.min(new_len) - start);
            let line = before.char_to_line(start);
            let line_start = before.line_to_char(line) == start;
            let removed = before.slice(start..old_len - tail).to_string();
            let inserted = after.slice(start..new_len - tail).to_string();
            self.shift(&[LineEdit::new(line, line_start, &removed, &inserted)]);
        }
        self.settle(lines, Stale::Whole);
    }

    fn copy(&self, cx: &mut App) {
        let rope = self.buffer.rope();
        let parts: Vec<String> = self
            .carets
            .iter()
            .filter_map(|caret| rope.get_slice(caret.range()))
            .map(|slice| slice.to_string())
            .filter(|part| !part.is_empty())
            .collect();
        if !parts.is_empty() {
            cx.write_to_clipboard(ClipboardItem::new_string(parts.join("\n")));
        }
    }

    fn paste(&mut self, cx: &mut App) {
        if let Some(text) = cx.read_from_clipboard().and_then(|item| item.text()) {
            self.insert(&text);
        }
    }

    fn touch(&mut self, cx: &mut Context<Self>) {
        if self.edited_since_found {
            self.search(cx);
        }
        self.since = Instant::now();
        self.blink = None;
        cx.notify();
    }

    fn reveal(&mut self) {
        self.unfold_carets();
        if let Some(caret) = self.carets.last()
            && let Ok(line) = self.buffer.char_to_line(caret.head)
        {
            let row = match self.wrapped() {
                Some(wrap) => self.visual(caret.head, wrap).map_or(line, |(row, _)| row),
                None => self.row_of(line),
            };
            reveal_row(&self.scroll, &self.heights, row);
        }
    }

    fn key_down(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let keys = &event.keystroke;
        if keys.modifiers.secondary() && self.frames.is_some() {
            eprintln!(
                "desk: editor key {} char {:?}",
                keys.unparse(),
                keys.key_char
            );
        }
        let command = match (vim::key(keys), self.vim.as_mut()) {
            (Some(key), Some(vim)) => vim.key(key),
            _ => vim::Command::Pass,
        };
        if command != vim::Command::Pass {
            self.obey(command, window, cx);
            self.vim_clamp();
            self.publish_keys(Vec::new(), cx);
            cx.stop_propagation();
            self.reveal();
            return self.touch(cx);
        }
        let mut typed = cx
            .try_global::<FocusedKeys>()
            .filter(|keys| keys.focus == self.focus)
            .map(|keys| keys.pending.clone())
            .unwrap_or_default();
        typed.push(Stroke::typed(keys));
        let resolved = self.keymap.resolve(&typed);
        let waiting = matches!(resolved, Resolved::Wait);
        self.publish_keys(if waiting { typed.clone() } else { Vec::new() }, cx);
        let action = match resolved {
            Resolved::Run(action) => action,
            Resolved::Wait => return cx.stop_propagation(),
            Resolved::Miss if typed.len() > 1 => {
                let chord: Vec<String> = typed.iter().map(ToString::to_string).collect();
                eprintln!("desk: editor chord {} is not bound", chord.join(" "));
                return cx.stop_propagation();
            }
            Resolved::Miss => {
                let Some(jump) = self.keyed_jump(keys, window, cx) else {
                    return;
                };
                self.leap(jump, window, cx);
                return cx.stop_propagation();
            }
        };
        if let Action::Host(host) = action
            && !window.is_action_available(host.action().as_ref(), cx)
        {
            return;
        }
        eprintln!("desk: editor action {action}");
        self.run(action, window, cx);
        self.vim_clamp();
        cx.stop_propagation();
        self.reveal();
        self.touch(cx);
    }

    fn publish_keys(&self, pending: Vec<Stroke>, cx: &mut App) {
        cx.set_global(FocusedKeys {
            focus: self.focus.clone(),
            keymap: self.keymap.clone(),
            pending,
            vim: self.vim.as_ref().map(Vim::mode),
        });
    }

    fn head(&self) -> usize {
        self.carets.last().map_or(0, |caret| caret.head)
    }

    fn line_at(&self, at: usize) -> usize {
        self.buffer.char_to_line(at).unwrap_or(0)
    }

    fn put_head(&mut self, head: usize) {
        let anchor = match self.vim.as_ref().map(Vim::mode) {
            Some(Mode::Visual | Mode::VisualLine) => self.carets.last().map_or(head, |c| c.anchor),
            _ => head,
        };
        self.carets = vec![Caret {
            anchor,
            head,
            goal: None,
        }];
    }

    fn vim_clamp(&mut self) {
        let Some(mode) = self.vim.as_ref().map(Vim::mode) else {
            return;
        };
        if mode == Mode::Insert {
            return;
        }
        let ends: Vec<Option<Range<usize>>> = self
            .carets
            .iter()
            .map(|caret| vim::line_span(&self.buffer, self.line_at(caret.head)))
            .collect();
        for (caret, line) in self.carets.iter_mut().zip(ends) {
            if let Some(line) = line.filter(|line| !line.is_empty() && caret.head >= line.end) {
                caret.head = line.end - 1;
            }
            if !mode.visual() {
                caret.anchor = caret.head;
            }
        }
    }

    fn painted(&self, caret: Caret) -> Range<usize> {
        let line_of = |at: usize| vim::line_span(&self.buffer, self.line_at(at));
        match self.vim.as_ref().map(Vim::mode) {
            None | Some(Mode::Insert) => caret.range(),
            Some(Mode::VisualLine) => self.selection(caret, true),
            Some(Mode::Visual) => self.selection(caret, false),
            Some(Mode::Normal | Mode::Pending | Mode::Command) => {
                let end = line_of(caret.head).map_or(caret.head, |line| line.end);
                caret.head..(caret.head + 1).min(end).max(caret.head)
            }
        }
    }

    fn selection(&self, caret: Caret, linewise: bool) -> Range<usize> {
        let range = caret.range();
        if !linewise {
            return range.start..self.after(range.end);
        }
        let start = vim::line_span(&self.buffer, self.line_at(range.start));
        let next = self.line_at(range.end) + 1;
        let end = self
            .buffer
            .line_to_char(next)
            .unwrap_or(self.buffer.len_chars());
        start.map_or(range.start, |line| line.start)..end
    }

    fn obey(&mut self, command: vim::Command, window: &mut Window, cx: &mut Context<Self>) {
        let head = self.head();
        match command {
            vim::Command::Pass | vim::Command::Held => {}
            vim::Command::Move(motion, count) => {
                if let Some(frames) = &self.frames {
                    frames.borrow_mut().mark("vim motion");
                }
                self.vim_move(motion, count);
            }
            vim::Command::Operate(operator, target, count) => {
                match vim::span(&self.buffer, head, operator, target, count) {
                    Some(span) => self.vim_operate(operator, span),
                    None => {
                        if let Some(vim) = self.vim.as_mut() {
                            vim.enter(Mode::Normal);
                        }
                    }
                }
            }
            vim::Command::Selected(operator, linewise) => {
                let caret = self.carets.last().copied().unwrap_or(Caret::at(head));
                let chars = self.selection(caret, linewise);
                self.vim_operate(operator, vim::Span { chars, linewise });
            }
            vim::Command::Grab(object) => {
                let target = vim::Target::Object(object);
                if let Some(span) = vim::span(&self.buffer, head, vim::Operator::Yank, target, 1)
                    && !span.chars.is_empty()
                {
                    self.carets = vec![Caret {
                        anchor: span.chars.start,
                        head: span.chars.end - 1,
                        goal: None,
                    }];
                }
            }
            vim::Command::Insert(entry) => self.vim_insert(entry),
            vim::Command::Paste { before, count } => self.vim_paste(before, count),
            vim::Command::Undo(count) => (0..count).for_each(|_| self.history(true)),
            vim::Command::Redo(count) => (0..count).for_each(|_| self.history(false)),
            vim::Command::Repeat(count) => self.vim_repeat(count, window, cx),
            vim::Command::Select | vim::Command::Unselect => self.carets = vec![Caret::at(head)],
            vim::Command::Normal => {
                let line = vim::line_span(&self.buffer, self.line_at(head));
                if let Some(vim) = self.vim.as_mut() {
                    let from = vim.insert_from();
                    let typed = self.buffer.rope().get_slice(from..head.max(from));
                    vim.finished_insert(typed.map(|typed| typed.to_string()).unwrap_or_default());
                }
                let back = line.is_some_and(|line| head > line.start);
                self.carets = vec![Caret::at(head - usize::from(back))];
            }
            vim::Command::Ex(ex) => self.vim_ex(ex, window, cx),
        }
    }

    fn vim_move(&mut self, motion: vim::Motion, count: usize) {
        let extend = self.vim.as_ref().is_some_and(|vim| vim.mode().visual());
        let step = match motion {
            vim::Motion::Up => "up",
            vim::Motion::Down => "down",
            _ => {
                if let Some(reach) = vim::reach(&self.buffer, self.head(), motion, count) {
                    self.put_head(reach.to);
                }
                return;
            }
        };
        for _ in 0..count {
            let (before, line) = (self.carets.clone(), self.line_at(self.head()));
            self.travel(step, false, extend);
            if self.wrapped().is_none() && self.line_at(self.head()) == line {
                self.carets = before;
                break;
            }
        }
    }

    fn vim_operate(&mut self, operator: vim::Operator, span: vim::Span) {
        let mut chars = span.chars;
        let mut text = self
            .buffer
            .rope()
            .get_slice(chars.clone())
            .map(|text| text.to_string())
            .unwrap_or_default();
        if span.linewise && !text.ends_with('\n') {
            text.push('\n');
        }
        let first = self.line_at(chars.start);
        if let Some(vim) = self.vim.as_mut() {
            vim.yank(text, span.linewise);
        }
        match (operator, span.linewise) {
            (vim::Operator::Yank, linewise) => {
                let at = if linewise { self.head() } else { chars.start };
                self.carets = vec![Caret::at(at)];
            }
            (vim::Operator::Delete, false) => self.apply(std::slice::from_ref(&chars), ""),
            (vim::Operator::Delete, true) => {
                let ends_file = chars.end >= self.buffer.len_chars();
                if ends_file && chars.start > 0 {
                    chars.start = self.before(chars.start);
                }
                self.apply(std::slice::from_ref(&chars), "");
                let line = first.min(self.buffer.line_count().saturating_sub(1));
                let at = vim::first_word(&self.buffer, line).unwrap_or(0);
                self.carets = vec![Caret::at(at)];
            }
            (vim::Operator::Change, linewise) => {
                if linewise {
                    let last = self.line_at(chars.end.saturating_sub(1).max(chars.start));
                    let end = vim::line_span(&self.buffer, last).map_or(chars.end, |l| l.end);
                    let indent = vim::first_word(&self.buffer, first).unwrap_or(chars.start);
                    chars = indent.min(end)..end;
                }
                self.apply(std::slice::from_ref(&chars), "");
                self.carets = vec![Caret::at(chars.start)];
                if let Some(vim) = self.vim.as_mut() {
                    vim.began_insert(chars.start);
                }
            }
        }
    }

    fn put(&mut self, at: usize, text: &str) {
        self.apply(std::slice::from_ref(&(at..at)), text);
    }

    fn vim_insert(&mut self, entry: vim::Entry) {
        let head = self.head();
        let line = self.line_at(head);
        let Some(span) = vim::line_span(&self.buffer, line) else {
            return;
        };
        let indent = self
            .buffer
            .rope()
            .get_slice(span.start..vim::first_word(&self.buffer, line).unwrap_or(span.start))
            .map(|indent| indent.to_string())
            .unwrap_or_default();
        let at = match entry {
            vim::Entry::Before => head,
            vim::Entry::After => (head + 1).min(span.end),
            vim::Entry::LineStart => vim::first_word(&self.buffer, line).unwrap_or(span.start),
            vim::Entry::LineEnd => span.end,
            vim::Entry::Below => {
                self.put(span.end, &format!("\n{indent}"));
                self.head()
            }
            vim::Entry::Above => {
                self.put(span.start, &format!("{indent}\n"));
                span.start + indent.chars().count()
            }
        };
        self.carets = vec![Caret::at(at)];
        if let Some(vim) = self.vim.as_mut() {
            vim.began_insert(at);
        }
    }

    fn vim_paste(&mut self, before: bool, count: usize) {
        let Some((text, linewise)) = self.vim.as_ref().and_then(Vim::register) else {
            return;
        };
        let text = text.repeat(count);
        let head = self.head();
        let line = self.line_at(head);
        let Some(span) = vim::line_span(&self.buffer, line) else {
            return;
        };
        if !linewise {
            let at = if before {
                head
            } else {
                (head + 1).min(span.end)
            };
            self.put(at, &text);
            let end = self.head();
            self.carets = vec![Caret::at(end.saturating_sub(1).max(at))];
            return;
        }
        let below = line + 1;
        let (at, text, landed) = match (before, self.buffer.line_to_char(below)) {
            (true, _) => (span.start, text, line),
            (false, Ok(next)) if below < self.buffer.line_count() => (next, text, below),
            (false, _) => {
                let body = text.strip_suffix('\n').unwrap_or(&text);
                (self.buffer.len_chars(), format!("\n{body}"), below)
            }
        };
        self.put(at, &text);
        let at = vim::first_word(&self.buffer, landed).unwrap_or(at);
        self.carets = vec![Caret::at(at)];
    }

    fn vim_repeat(&mut self, count: usize, window: &mut Window, cx: &mut Context<Self>) {
        let Some((keys, text)) = self.vim.as_ref().map(Vim::last_change) else {
            return;
        };
        if keys.is_empty() {
            return;
        }
        for _ in 0..count {
            for key in keys.iter().chain([&vim::Key::Escape]) {
                let Some(vim) = self.vim.as_mut() else {
                    return;
                };
                if *key == vim::Key::Escape && vim.mode() == Mode::Insert {
                    self.insert(&text);
                }
                let Some(vim) = self.vim.as_mut() else {
                    return;
                };
                let command = vim.key(*key);
                self.obey(command, window, cx);
            }
        }
    }

    fn vim_ex(&mut self, ex: vim::Ex, window: &mut Window, cx: &mut Context<Self>) {
        match ex {
            vim::Ex::Write => self.run(Action::Save, window, cx),
            vim::Ex::Quit => self.run(Action::Host(Host::CloseTab), window, cx),
            vim::Ex::WriteQuit => {
                self.run(Action::Save, window, cx);
                self.run(Action::Host(Host::CloseTab), window, cx);
            }
            vim::Ex::Line(line) => {
                let last = self.buffer.line_count().saturating_sub(1);
                let line = line.saturating_sub(1).min(last);
                self.carets = vec![Caret::at(vim::first_word(&self.buffer, line).unwrap_or(0))];
            }
            vim::Ex::Unknown(text) => eprintln!("desk: vim :{text} is not a command"),
        }
    }

    fn run(&mut self, action: Action, window: &mut Window, cx: &mut Context<Self>) {
        match action {
            Action::Move(motion) => self.go(motion, false),
            Action::Select(motion) => self.go(motion, true),
            Action::AddCursorUp => self.stack("up"),
            Action::AddCursorDown => self.stack("down"),
            Action::SelectAll => {
                self.carets = vec![Caret {
                    head: self.buffer.len_chars(),
                    ..Caret::at(0)
                }]
            }
            Action::SingleCursor => {
                self.carets = self
                    .carets
                    .last()
                    .map(|caret| Caret::at(caret.head))
                    .into_iter()
                    .collect();
            }
            Action::DeleteLeft => self.erase(true),
            Action::DeleteRight => self.erase(false),
            Action::Newline => self.insert("\n"),
            Action::Tab if self.spans_lines() => self.indent(),
            Action::Tab => self.insert(&self.to_tab_stop()),
            Action::DeleteLine => self.delete_lines(),
            Action::DuplicateLine => self.duplicate_lines(),
            Action::MoveLineUp => self.shift_lines(true),
            Action::MoveLineDown => self.shift_lines(false),
            Action::CommentToggle => self.comment_lines(),
            Action::Indent => self.indent(),
            Action::Outdent => self.edit_lines(|text| {
                let gone = match text.starts_with('\t') {
                    true => 1,
                    false => text
                        .chars()
                        .take(TAB.len())
                        .take_while(|ch| *ch == ' ')
                        .count(),
                };
                (gone > 0).then(|| (0, gone, String::new()))
            }),
            Action::Find => self.find(window, cx),
            Action::FindReplace => {
                self.find(window, cx);
                if let Some(bar) = self.bar.clone() {
                    bar.update(cx, |bar, cx| bar.show_replace(window, cx));
                }
            }
            Action::AddNextMatch => self.add_next_match(),
            Action::Save => {
                if let Some(save) = self.on_save.clone().filter(|_| !self.read_only()) {
                    let saved = save(&mut self.buffer, window, cx);
                    self.failure = saved.err().map(|error| ("Could not save", error.into()));
                }
            }
            Action::Fold => self.fold_at_caret(),
            Action::Unfold => {
                if let Some(line) = self.caret_line() {
                    self.unfold(line);
                }
            }
            Action::Undo => self.history(true),
            Action::Redo => self.history(false),
            Action::Copy => self.copy(cx),
            Action::Cut => {
                self.copy(cx);
                self.insert("");
            }
            Action::Paste => self.paste(cx),
            Action::ToggleWrap => self.toggle_wrap(),
            Action::Host(host) => window.dispatch_action(host.action(), cx),
        }
    }

    fn go(&mut self, motion: Motion, extend: bool) {
        let (key, word) = match motion {
            Motion::Left => ("left", false),
            Motion::Right => ("right", false),
            Motion::WordLeft => ("left", true),
            Motion::WordRight => ("right", true),
            Motion::Up => ("up", false),
            Motion::Down => ("down", false),
            Motion::LineStart => ("home", false),
            Motion::LineEnd => ("end", false),
            Motion::Top => ("home", true),
            Motion::Bottom => ("end", true),
        };
        self.travel(key, word, extend);
    }

    fn spans_lines(&self) -> bool {
        self.carets.iter().any(|caret| {
            let range = caret.range();
            self.buffer.char_to_line(range.start).ok() != self.buffer.char_to_line(range.end).ok()
        })
    }

    fn caret_lines(&self) -> Vec<Range<usize>> {
        let mut groups: Vec<Range<usize>> = self
            .carets
            .iter()
            .filter_map(|caret| {
                let range = caret.range();
                let first = self.buffer.char_to_line(range.start).ok()?;
                let last = self.buffer.char_to_line(range.end).ok()?;
                let ends_before =
                    last > first && self.buffer.line_to_char(last).ok() == Some(range.end);
                Some(first..if ends_before { last } else { last + 1 })
            })
            .collect();
        groups.sort_by_key(|lines| lines.start);
        let mut merged: Vec<Range<usize>> = Vec::with_capacity(groups.len());
        for lines in groups {
            match merged.last_mut() {
                Some(last) if lines.start <= last.end => last.end = last.end.max(lines.end),
                _ => merged.push(lines),
            }
        }
        merged
    }

    fn place(&self, at: usize) -> Option<(usize, usize)> {
        let line = self.buffer.char_to_line(at).ok()?;
        Some((line, at - self.buffer.line_to_char(line).ok()?))
    }

    fn at_place(&self, (line, column): (usize, usize)) -> usize {
        match self.buffer.line_to_char(line) {
            Ok(start) => start + column.min(self.line_len(line)),
            Err(_) => self.buffer.len_chars(),
        }
    }

    fn rewrite(
        &mut self,
        lines: Range<usize>,
        text: &[String],
        moved: impl Fn(usize, usize) -> (usize, usize),
    ) {
        let last_open = lines.end >= self.buffer.line_count();
        let end = match last_open {
            true => Ok(self.buffer.len_chars()),
            false => self.buffer.line_to_char(lines.end),
        };
        let (Ok(start), Ok(end), false) =
            (self.buffer.line_to_char(lines.start), end, self.read_only())
        else {
            return;
        };
        let joined: String = match last_open {
            true => text.join("\n"),
            false => text.iter().map(|line| format!("{line}\n")).collect(),
        };
        let places: Vec<_> = self
            .carets
            .iter()
            .filter_map(|caret| Some((self.place(caret.anchor)?, self.place(caret.head)?)))
            .collect();
        self.apply(std::slice::from_ref(&(start..end)), &joined);
        self.carets = places
            .into_iter()
            .map(|((line, column), (head_line, head_column))| Caret {
                anchor: self.at_place(moved(line, column)),
                head: self.at_place(moved(head_line, head_column)),
                goal: None,
            })
            .collect();
        self.merge();
    }

    fn texts(&self, lines: impl Iterator<Item = usize>) -> Vec<String> {
        lines.filter_map(|line| self.buffer.line(line)).collect()
    }

    fn delete_lines(&mut self) {
        let groups = self.caret_lines();
        let (Some(first), Some(last)) = (groups.first(), groups.last()) else {
            return;
        };
        let mut span = first.start..last.end;
        let kept = span
            .clone()
            .filter(|line| !groups.iter().any(|lines| lines.contains(line)));
        let mut text = self.texts(kept);
        if text.is_empty() && span.end >= self.buffer.line_count() && span.start > 0 {
            span.start -= 1;
            text = self.texts(span.start..span.start + 1);
        }
        let gone = |line: usize| -> usize {
            groups
                .iter()
                .map(|lines| lines.end.min(line).saturating_sub(lines.start))
                .sum()
        };
        self.rewrite(span, &text, |line, column| (line - gone(line), column));
    }

    fn duplicate_lines(&mut self) {
        let groups = self.caret_lines();
        let (Some(first), Some(last)) = (groups.first(), groups.last()) else {
            return;
        };
        let span = first.start..last.end;
        let order = span.clone().flat_map(|line| {
            let copy = groups.iter().find(|lines| lines.end == line + 1);
            std::iter::once(line).chain(copy.cloned().unwrap_or_default())
        });
        let text = self.texts(order);
        let added = |line: usize| -> usize {
            groups
                .iter()
                .filter(|lines| lines.start <= line)
                .map(ExactSizeIterator::len)
                .sum()
        };
        self.rewrite(span, &text, |line, column| (line + added(line), column));
    }

    fn shift_lines(&mut self, up: bool) {
        let groups = self.caret_lines();
        let (Some(first), Some(last)) = (groups.first(), groups.last()) else {
            return;
        };
        let span = match up {
            true => first.start.checked_sub(1).map(|above| above..last.end),
            false => (last.end < self.buffer.line_count()).then_some(first.start..last.end + 1),
        };
        let Some(span) = span else {
            return;
        };
        let mut order: Vec<usize> = span.clone().collect();
        for lines in &groups {
            let around = match up {
                true => lines.start - 1 - span.start..lines.end - span.start,
                false => lines.start - span.start..lines.end + 1 - span.start,
            };
            if let Some(turned) = order.get_mut(around) {
                match up {
                    true => turned.rotate_left(1),
                    false => turned.rotate_right(1),
                }
            }
        }
        let text = self.texts(order.into_iter());
        self.rewrite(span, &text, |line, column| {
            let moved = groups.iter().find_map(|lines| match up {
                true if lines.contains(&line) => Some(line - 1),
                true if line + 1 == lines.start => Some(lines.end - 1),
                false if lines.contains(&line) => Some(line + 1),
                false if line == lines.end => Some(lines.start),
                _ => None,
            });
            (moved.unwrap_or(line), column)
        });
    }

    fn edit_lines(&mut self, change: impl Fn(&str) -> Option<(usize, usize, String)>) {
        let groups = self.caret_lines();
        let (Some(first), Some(last)) = (groups.first(), groups.last()) else {
            return;
        };
        let span = first.start..last.end;
        let mut cuts = BTreeMap::new();
        let mut text = self.texts(span.clone());
        for (line, content) in span.clone().zip(text.iter_mut()) {
            let picked = groups.iter().any(|lines| lines.contains(&line));
            let Some((at, gone, added)) = picked.then(|| change(content)).flatten() else {
                continue;
            };
            let byte = |column: usize| {
                content
                    .char_indices()
                    .nth(column)
                    .map_or(content.len(), |(byte, _)| byte)
            };
            let bytes = byte(at)..byte(at + gone);
            content.replace_range(bytes, &added);
            cuts.insert(line, (at, gone, added.chars().count()));
        }
        if cuts.is_empty() {
            return;
        }
        self.rewrite(span, &text, |line, column| match cuts.get(&line) {
            Some(&(at, gone, added)) if column >= at + gone => (line, column - gone + added),
            Some(&(at, _, _)) if column > at => (line, at),
            _ => (line, column),
        });
    }

    fn tab(&self) -> &'static str {
        let (mut tabs, mut spaces) = (0usize, 0usize);
        for line in self.buffer.rope().lines() {
            match line.chars().next() {
                Some('\t') => tabs += 1,
                Some(' ') => spaces += 1,
                _ => {}
            }
        }
        match self.language == Some(Language::Go) || tabs > spaces {
            true => "\t",
            false => TAB,
        }
    }

    fn to_tab_stop(&self) -> String {
        let tab = self.tab();
        if tab == "\t" {
            return tab.to_owned();
        }
        let column = self.carets.last().and_then(|caret| {
            let (line, column) = self.place(caret.head)?;
            let drawn = self.drawn(line);
            Some(drawn.text.get(..drawn.byte(column))?.chars().count())
        });
        " ".repeat(TAB_STOP - column.unwrap_or(0) % TAB_STOP)
    }

    fn indent(&mut self) {
        let tab = self.tab();
        self.edit_lines(|text| (!text.is_empty()).then(|| (0, 0, tab.to_owned())));
    }

    fn comment_lines(&mut self) {
        let Some(token) = self.comment else {
            eprintln!("desk: editor has no line comment for this file");
            return;
        };
        let lines = self.texts(self.caret_lines().into_iter().flatten());
        let filled = lines.iter().filter(|line| !line.trim().is_empty());
        let commented = filled
            .clone()
            .all(|line| line.trim_start().starts_with(token));
        let lead = |text: &str| text.chars().take_while(|ch| ch.is_whitespace()).count();
        let indent = filled.map(|line| lead(line)).min().unwrap_or(0);
        self.edit_lines(|text| {
            let rest = text.trim_start();
            match (rest.is_empty(), commented) {
                (true, _) => None,
                (false, true) => {
                    let after = rest.strip_prefix(token)?;
                    Some((
                        lead(text),
                        token.len() + usize::from(after.starts_with(' ')),
                        String::new(),
                    ))
                }
                (false, false) => Some((indent, 0, format!("{token} "))),
            }
        });
    }

    fn char_at_point(&self, position: Point<Pixels>, window: &Window, cx: &App) -> Option<usize> {
        let (row, x) = code_place(self.bounds.get(), &self.scroll, &self.heights, position);
        let wrap = self.wrapped();
        let (line, piece) = match wrap {
            Some(wrap) => wrap.locate(row.min(wrap.rows().saturating_sub(1))),
            None => (self.line_of(row), 0),
        };
        let drawn = self.drawn(line);
        let text = &drawn.text;
        let (within, last) = match wrap {
            Some(wrap) => (
                wrap.piece(line, piece, text.len()),
                piece + 1 == wrap.pieces(line),
            ),
            None => (0..text.len(), true),
        };
        let shown: SharedString = text.get(within.clone())?.to_owned().into();
        let theme = ActiveTheme::theme(cx);
        let mut byte =
            within.start + code_shape(shown, drawn.heading, window, &theme).closest_index_for_x(x);
        if !last && byte >= within.end {
            byte = text
                .get(..within.end)?
                .char_indices()
                .last()
                .map_or(within.start, |(at, _)| at.max(within.start));
        }
        Some(self.buffer.line_to_char(line).ok()? + drawn.column(byte))
    }

    fn x_of(&self, at: usize, window: &Window, cx: &App) -> Option<(usize, Pixels)> {
        let line = self.buffer.char_to_line(at).ok()?;
        let column = at - self.buffer.line_to_char(line).ok()?;
        let drawn = self.drawn(line);
        let text = drawn.text.as_str();
        let byte = drawn.byte(column);
        let (row, within) = match self.wrapped() {
            Some(wrap) => {
                let row = wrap.row_of(line, byte);
                (row, wrap.piece(line, wrap.locate(row).1, text.len()))
            }
            None => (self.row_of(line), 0..text.len()),
        };
        let piece: SharedString = text.get(within.clone())?.to_owned().into();
        let x = code_shape(piece, drawn.heading, window, &ActiveTheme::theme(cx))
            .x_for_index(byte.saturating_sub(within.start));
        Some((row, x))
    }

    fn link_line(&self, window: &Window, cx: &App) -> Option<(usize, Pixels, Pixels)> {
        let link = self.link.as_ref()?;
        let (row, from) = self.x_of(link.start, window, cx)?;
        let (end_row, to) = self.x_of(link.end, window, cx)?;
        (row == end_row && to > from).then_some((row, from, to))
    }

    fn mouse_down(&mut self, event: &MouseDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let since = Instant::now();
        window.focus(&self.focus, cx);
        self.publish_keys(Vec::new(), cx);
        if self.gutter_fold(event.position) {
            cx.notify();
            return;
        }
        let Some(at) = self.char_at_point(event.position, window, cx) else {
            return;
        };
        let held = event.modifiers;
        let follows = held.secondary() && !held.alt && !held.shift && self.in_text(event.position);
        if follows
            && self.on_jump.is_some()
            && let Some(jump) = self.definition_jump(at, event.position, since)
        {
            self.leap(jump, window, cx);
            return cx.stop_propagation();
        }
        match self.carets.last_mut() {
            Some(last) if held.shift && !held.alt => {
                last.head = at;
                last.goal = None;
            }
            _ if held.alt => self.carets.push(Caret::at(at)),
            _ => self.carets = vec![Caret::at(at)],
        }
        self.marked = None;
        self.dragging = true;
        self.merge();
        self.touch(cx);
    }

    fn mouse_move(&mut self, event: &MouseMoveEvent, window: &mut Window, cx: &mut Context<Self>) {
        if !self.dragging || !event.dragging() {
            self.hover(event.position, cx);
            self.point_link(event.position, event.modifiers.secondary(), window, cx);
            return;
        }
        let Some(at) = self.char_at_point(event.position, window, cx) else {
            return;
        };
        if let Some(last) = self.carets.last_mut()
            && last.head != at
        {
            last.head = at;
            last.goal = None;
            self.merge();
            self.touch(cx);
        }
    }

    fn release(&mut self, _: &MouseUpEvent, _: &mut Window, _: &mut Context<Self>) {
        self.dragging = false;
    }

    fn rows(&self, rows: Range<usize>, caret_on: bool) -> Vec<CodeLine> {
        let mut lines = match self.live {
            true => self.live_lines(rows.clone()),
            false => coloured_lines(&self.buffer, self.syntax.spans(rows.clone()), rows.clone()),
        };
        let current = self.found.get(self.current);
        let marking = !matches!(
            self.vim.as_ref().map(Vim::mode),
            Some(Mode::Normal | Mode::Pending | Mode::Command)
        );
        let rope = self.buffer.rope();
        for (row, (line, drawn)) in rows.zip(lines.iter_mut()) {
            let Ok(start) = self.buffer.line_to_char(row) else {
                continue;
            };
            if self.guides {
                line.guides = Guides {
                    levels: self.levels(row),
                    active: self
                        .active_guide
                        .as_ref()
                        .filter(|(_, block)| block.contains(&row))
                        .map(|(guide, _)| *guide),
                };
            }
            let end = start + drawn.columns();
            let byte_of = |at: usize| drawn.byte(at - start);
            let first = self.found.partition_point(|found| found.end <= start);
            for found in self.found.iter().skip(first) {
                let (from, to) = (found.start.max(start), found.end.min(end));
                if from >= to {
                    break;
                }
                if Some(found) == current {
                    line.current_match = Some(line.matches.len());
                }
                line.matches.push(byte_of(from)..byte_of(to));
            }
            for caret in &self.carets {
                if caret_on && (start..=end).contains(&caret.head) {
                    line.carets.push(byte_of(caret.head));
                }
                let range = self.painted(*caret);
                let (from, to) = (range.start.max(start), range.end.min(end));
                if from < to && Some(&range) != current {
                    line.selected.push(byte_of(from)..byte_of(to));
                }
                if from < to && marking {
                    line.blanks.extend((from..to).filter_map(|at| {
                        let blank = match rope.get_char(at)? {
                            ' ' => Blank::Space,
                            '\t' => Blank::Tab,
                            _ => return None,
                        };
                        Some((byte_of(at)..byte_of(at + 1), blank))
                    }));
                }
            }
        }
        lines.into_iter().map(|(line, _)| line).collect()
    }

    fn lit_marks(&self, editor: &WeakEntity<Self>) -> Rc<Marks> {
        let lines: Vec<usize> = self
            .carets
            .iter()
            .filter_map(|caret| {
                let range = caret.range();
                let line = self.buffer.char_to_line(range.start).ok()?;
                (self.buffer.char_to_line(range.end).ok()? == line).then_some(line)
            })
            .collect();
        if lines.is_empty() && self.folds.is_empty() && self.chevron.is_none() {
            return self.marks.clone();
        }
        let mut marks = (*self.marks).clone();
        for line in lines {
            marks
                .entry(line)
                .or_default()
                .background
                .get_or_insert(CURRENT_LINE);
        }
        if let Some(line) = self.chevron {
            marks.entry(line).or_default().fold = Some(FoldMark::Open);
        }
        for &header in self.folds.keys() {
            let mark = marks.entry(header).or_default();
            mark.fold = Some(FoldMark::Folded);
            let (editor, before) = (editor.clone(), mark.trailing.take());
            mark.trailing = Some(Rc::new(move |theme: &Theme| {
                div()
                    .flex()
                    .items_center()
                    .child(fold_pill(header, editor.clone(), theme))
                    .children(before.as_ref().map(|before| before(theme)))
                    .into_any_element()
            }));
        }
        Rc::new(marks)
    }

    fn chars(&self, units: Range<usize>) -> Range<usize> {
        let rope = self.buffer.rope();
        let char_of = |unit: usize| self.snap(rope.utf16_cu_to_char(unit.min(rope.len_utf16_cu())));
        let (start, end) = (char_of(units.start), char_of(units.end));
        start.min(end)..start.max(end)
    }

    fn units(&self, at: usize) -> usize {
        let rope = self.buffer.rope();
        rope.char_to_utf16_cu(at.min(rope.len_chars()))
    }
}

impl Focusable for CodeEditor {
    fn focus_handle(&self, _: &App) -> FocusHandle {
        self.focus.clone()
    }
}

impl EntityInputHandler for CodeEditor {
    fn text_for_range(
        &mut self,
        units: Range<usize>,
        actual: &mut Option<Range<usize>>,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<String> {
        let chars = self.chars(units);
        actual.replace(self.units(chars.start)..self.units(chars.end));
        self.buffer
            .rope()
            .get_slice(chars)
            .map(|slice| slice.to_string())
    }

    fn selected_text_range(
        &mut self,
        _: bool,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<UTF16Selection> {
        let caret = self.carets.last()?;
        let range = caret.range();
        Some(UTF16Selection {
            range: self.units(range.start)..self.units(range.end),
            reversed: caret.head < caret.anchor,
        })
    }

    fn marked_text_range(&self, _: &mut Window, _: &mut Context<Self>) -> Option<Range<usize>> {
        let marked = self.marked.as_ref()?;
        Some(self.units(marked.start)..self.units(marked.end))
    }

    fn unmark_text(&mut self, _: &mut Window, cx: &mut Context<Self>) {
        self.marked = None;
        cx.notify();
    }

    fn replace_text_in_range(
        &mut self,
        units: Option<Range<usize>>,
        text: &str,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let targets = match (units, self.marked.take()) {
            (Some(units), _) => vec![self.chars(units)],
            (None, Some(marked)) => vec![marked],
            (None, None) => self.selections(),
        };
        self.apply(&targets, text);
        self.reveal();
        self.touch(cx);
    }

    fn replace_and_mark_text_in_range(
        &mut self,
        units: Option<Range<usize>>,
        text: &str,
        caret: Option<Range<usize>>,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let target = match (units, self.marked.take()) {
            (Some(units), _) => self.chars(units),
            (None, Some(marked)) => marked,
            (None, None) => self.carets.last().map_or(0..0, |caret| caret.range()),
        };
        self.apply(std::slice::from_ref(&target), text);
        let start = target.start;
        let typed = text.chars().count();
        self.marked = (typed > 0).then_some(start..start + typed);
        if let Some(caret) = caret {
            self.carets = vec![Caret {
                anchor: start + chars_within(text, caret.start),
                head: start + chars_within(text, caret.end),
                goal: None,
            }];
        }
        self.reveal();
        self.touch(cx);
    }

    fn bounds_for_range(
        &mut self,
        units: Range<usize>,
        element: Bounds<Pixels>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Option<Bounds<Pixels>> {
        let (row, x) = self.x_of(self.chars(units).start, window, cx)?;
        let origin = code_origin(element, &self.scroll, &self.heights, row);
        Some(Bounds::new(
            point(origin.x + x, origin.y),
            size(px(CARET_WIDTH), self.heights.height(row)),
        ))
    }

    fn character_index_for_point(
        &mut self,
        position: Point<Pixels>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Option<usize> {
        let at = self.char_at_point(position, window, cx)?;
        Some(self.units(at))
    }
}

impl Render for CodeEditor {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let started = Instant::now();
        let focused = self.focus.is_focused(window);
        let published = cx
            .try_global::<FocusedKeys>()
            .is_some_and(|keys| keys.focus == self.focus);
        if focused && !published {
            self.publish_keys(Vec::new(), cx);
        }
        match (focused, self.blink.is_some()) {
            (true, false) => self.blink = Some(blinker(cx)),
            (false, true) => self.blink = None,
            _ => {}
        }
        let shown = focused && caret_shown(self.since);
        if matches!(self.outlined, Outlined::Stale) {
            self.outline_later(cx);
        }
        let passes = Instant::now();
        self.rewrap(cx);
        self.remeasure();
        let passes = ms_since(passes);
        self.active_guide = self.guides.then(|| self.guide_block()).flatten();
        let paints = self.paints.clone();
        let painting = paints.clone().map(|paints| {
            canvas(|_, _, _| {}, move |_, _, _, _| paints.start())
                .absolute()
                .size_full()
        });
        let frames = self.frames.clone();
        if let Some(log) = frames.as_ref().filter(|log| log.borrow().edited.is_some()) {
            let log = log.clone();
            self.frames_flush = Some(cx.spawn(async move |_, cx| {
                cx.background_executor().timer(Duration::from_secs(1)).await;
                log.borrow_mut().report();
            }));
        }
        let entity = cx.entity();
        let source = entity.clone();
        let bounds = self.bounds.clone();
        let focus = self.focus.clone();
        let wrapped_at = self.wrapping.then(|| self.wrap.width());
        let folded_at = self.folded_at.clone();
        let shown_lines = self.shown_lines.clone();
        let link = self.link_line(window, cx);
        let link_ink = ActiveTheme::theme(cx).color(LINK);
        let scroll = self.scroll.clone();
        let heights = self.heights.clone();
        let rows = code_view(
            "code-editor-rows",
            self.buffer.line_count(),
            &self.scroll,
            move |rows, cx| {
                source.update(cx, |editor, _| {
                    let started = Instant::now();
                    let drawn = match &shown_lines {
                        None => editor.rows(rows, shown),
                        Some(lines) => rows
                            .filter_map(|row| lines.get(row))
                            .flat_map(|line| editor.rows(*line..*line + 1, shown))
                            .collect(),
                    };
                    if let Some(frames) = &editor.frames {
                        frames.borrow_mut().rows(started);
                    }
                    drawn
                })
            },
        )
        .widest(self.widest)
        .wrap(self.wrapped().map(|_| self.wrap.clone()))
        .shown(self.shown_lines.clone())
        .marks(self.lit_marks(&entity.downgrade()))
        .stripe(self.stripe(window))
        .heights(self.heights.clone());
        let reloaded = self.wrote.as_mut().and_then(|wrote| wrote.unlogged.take());
        let failure = self.failure.clone().map(|(what, error)| {
            div()
                .absolute()
                .left(px(NUMBER_COLUMN))
                .right(px(FAILURE_INSET))
                .bottom(px(FAILURE_INSET))
                .child(fail(what, error, "", &ActiveTheme::theme(cx)))
        });
        let pill = self.vim.as_ref().map(|vim| {
            let theme = ActiveTheme::theme(cx);
            div()
                .absolute()
                .right(px(FAILURE_INSET))
                .bottom(px(FAILURE_INSET))
                .px(px(PILL_PAD))
                .h(px(PILL_HEIGHT))
                .flex()
                .items_center()
                .rounded(px(PILL_RADIUS))
                .bg(theme.color(ColorToken::StateHover))
                .text_color(ink(&theme, T3))
                .child(vim.pill())
        });
        let editing = div()
            .id("code-editor")
            .map(|editing| match self.gutter {
                true => editing.relative().size_full(),
                false => editing
                    .absolute()
                    .top_0()
                    .bottom_0()
                    .right_0()
                    .left(px(BARE_PAD - NUMBER_COLUMN)),
            })
            .track_focus(&self.focus)
            .map(|editing| match link {
                Some(_) => editing.cursor_pointer(),
                None => editing.cursor_text(),
            })
            .on_key_down(cx.listener(Self::key_down))
            .on_modifiers_changed(cx.listener(Self::modifiers_changed))
            .on_mouse_down(MouseButton::Left, cx.listener(Self::mouse_down))
            .on_mouse_move(cx.listener(Self::mouse_move))
            .on_mouse_up(MouseButton::Left, cx.listener(Self::release))
            .on_mouse_up_out(MouseButton::Left, cx.listener(Self::release))
            .on_hover(cx.listener(|editor, hovered: &bool, _, cx| {
                if *hovered {
                    return;
                }
                let chevron = editor.chevron.take().is_some();
                if editor.link.take().is_some() || chevron {
                    cx.notify();
                }
            }))
            .children(painting)
            .child(rows)
            .child(
                canvas(
                    |_, _, _| {},
                    move |area, _, window, cx| {
                        if let Some(paints) = &paints {
                            paints.end();
                        }
                        bounds.set(area);
                        if let Some((row, from, to)) = link {
                            let origin = code_origin(area, &scroll, &heights, row);
                            let drop = heights.height(row) - px(LINK_DROP);
                            let start = point(origin.x + from, origin.y + drop);
                            if start.x >= area.left() + px(NUMBER_COLUMN) {
                                let line = Bounds::new(start, size(to - from, px(LINK_THICKNESS)));
                                window.paint_quad(fill(line, link_ink));
                            }
                        }
                        if let Some((logged, seen)) = reloaded {
                            let ms = seen.elapsed().as_secs_f64() * 1000.0;
                            eprintln!("{logged} in {ms:.1} ms");
                        }
                        if let Some(clicked) = folded_at.take() {
                            let ms = clicked.elapsed().as_secs_f64() * 1000.0;
                            eprintln!("desk: editor fold {ms:.2} ms");
                        }
                        if let Some(frames) = &frames {
                            frames.borrow_mut().painted();
                        }
                        if wrapped_at.is_some_and(|width| width != wrap_width(area)) {
                            cx.notify(entity.entity_id());
                        }
                        window.handle_input(&focus, ElementInputHandler::new(area, entity), cx);
                    },
                )
                .absolute()
                .top_0()
                .left_0()
                .size_full(),
            )
            .children(failure)
            .children(pill);
        if let Some(frames) = &self.frames {
            frames.borrow_mut().rendered(started, passes);
        }
        div()
            .relative()
            .size_full()
            .when(!self.gutter, |view| view.overflow_hidden())
            .child(editing)
            .children(self.bar.clone())
    }
}
