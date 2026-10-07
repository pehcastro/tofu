use std::rc::Rc;
use std::time::Instant;

use desk_motion::tokens::{EASE_OUT, HOVER_MS, TOGGLE_MS};
use gpui::{
    AnyElement, App, Div, ElementId, FontWeight, Motion, SharedString, Stateful, Window, div,
    prelude::*, px,
};

use crate::components::avatar::spinner;
use crate::components::button::{ButtonKind, button};
use crate::components::chip::{kbd, mono};
use crate::components::glyph::Glyph;
use crate::components::paint::{glyph, ink, ring, tint};
use crate::components::size::{
    BUTTON_GAP, COMPOSER_RING, FONT_BODY, FONT_SMALL, FONT_TAB, RADIUS_BADGE, RADIUS_ROW, ROW_ON,
    ROW_PAD_X, T2, T3,
};
use crate::metrics::ICON_TINY;
use crate::theme::{ColorToken, Theme};

const ASK_RADIUS: f32 = 13.0;
const ASK_PAD_X: f32 = 12.0;
const ASK_PAD_Y: f32 = 9.0;
const ASK_GAP: f32 = 8.0;
const WRAP_GAP: f32 = 4.0;
const ASK_BUTTON: f32 = 26.0;
const PRIMARY_KBD_FILL: f32 = 0.16;
const PRIMARY_KBD_TEXT: f32 = 0.72;
const PHASE_PAD_X: f32 = 14.0;
const PHASE_PAD_BOTTOM: f32 = 4.0;
const ACTIVE_RING: f32 = 0.24;
const FIELD_RING: f32 = 0.16;
const BOX_RING: f32 = 0.3;
const CHOICE_PAD_Y: f32 = 5.0;
const CHOICE_GAP: f32 = 2.0;
const CHECK_BOX: f32 = 14.0;
const FIELD_PAD_Y: f32 = 6.0;
const LONG_QUESTION: usize = 360;
const CLAMPED_LINES: usize = 3;
const MAX_KEYS: usize = 9;
const APPROVAL_IDS: usize = 100;
const MORE_ID: usize = 200;
const SEND_ID: usize = 201;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Answer {
    Allow,
    Deny,
    Always,
}

impl Answer {
    const ALL: [Answer; 3] = [Answer::Allow, Answer::Deny, Answer::Always];

    fn label(self) -> &'static str {
        match self {
            Answer::Allow => "Allow once",
            Answer::Deny => "Deny",
            Answer::Always => "Always here",
        }
    }

    fn said(self) -> &'static str {
        match self {
            Answer::Allow => "allowed once · building",
            Answer::Deny => "denied · the lead picks another way",
            Answer::Always => "always here · this kind of build runs without asking",
        }
    }
}

pub struct Ask {
    pub tool: SharedString,
    pub command: SharedString,
    pub writes: SharedString,
    pub hint: SharedString,
}

pub struct Choice {
    pub label: SharedString,
    pub about: Option<SharedString>,
}

pub struct Pick {
    pub choices: Vec<Choice>,
    pub many: bool,
    pub other: Option<SharedString>,
}

pub enum Shape {
    Approval(Ask),
    Pick(Pick),
    Typed(SharedString),
}

pub struct Question {
    pub header: SharedString,
    pub text: SharedString,
    pub shape: Shape,
}

impl Question {
    fn rows(&self) -> usize {
        match &self.shape {
            Shape::Approval(_) => Answer::ALL.len(),
            Shape::Pick(pick) => pick.rows(),
            Shape::Typed(_) => 0,
        }
    }

    fn long(&self) -> bool {
        self.text.chars().count() > LONG_QUESTION
    }

    fn told(&self, said: &Said) -> String {
        let picked = said.picked.iter().filter_map(|at| match &self.shape {
            Shape::Approval(_) => Answer::ALL.get(*at).map(|answer| answer.said()),
            Shape::Pick(pick) => pick.choices.get(*at).map(|choice| choice.label.as_ref()),
            Shape::Typed(_) => None,
        });
        let typed = (!said.typed.is_empty()).then(|| format!("\u{201c}{}\u{201d}", said.typed));
        let told = picked
            .map(str::to_owned)
            .chain(typed)
            .collect::<Vec<_>>()
            .join(", ");
        match self.shape {
            Shape::Approval(_) => told,
            Shape::Pick(_) | Shape::Typed(_) => format!("{} · {told}", self.header),
        }
    }
}

impl Pick {
    fn rows(&self) -> usize {
        self.choices.len() + usize::from(self.other.is_some())
    }

    fn other_at(&self, at: usize) -> bool {
        self.other.is_some() && at == self.choices.len()
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Act {
    Light(usize),
    Pick(usize),
    Back,
    Send,
    More,
}

struct Said {
    picked: Vec<usize>,
    typed: String,
}

pub struct Asking {
    questions: Rc<[Question]>,
    said: Vec<Said>,
    lit: usize,
    picked: Vec<usize>,
    typing: bool,
    more: bool,
    moved: Option<Instant>,
}

impl Asking {
    pub fn new(questions: Vec<Question>) -> Self {
        Asking::over(questions.into())
    }

    fn over(questions: Rc<[Question]>) -> Self {
        Asking {
            questions,
            said: Vec::new(),
            lit: 0,
            picked: Vec::new(),
            typing: false,
            more: false,
            moved: None,
        }
    }

    pub fn reset(&mut self, now: Instant) {
        *self = Asking {
            moved: Some(now),
            ..Asking::over(self.questions.clone())
        };
    }

    pub fn waiting(&self) -> bool {
        self.question().is_some()
    }

    pub fn moved(&self) -> Option<Instant> {
        self.moved
    }

    fn question(&self) -> Option<&Question> {
        self.questions.get(self.said.len())
    }

    pub fn placeholder(&self) -> Option<SharedString> {
        match &self.question()?.shape {
            Shape::Typed(placeholder) => Some(placeholder.clone()),
            Shape::Pick(Pick {
                other: Some(placeholder),
                ..
            }) if self.typing => Some(placeholder.clone()),
            Shape::Approval(_) | Shape::Pick(_) => None,
        }
    }

    pub fn typing(&self) -> bool {
        self.placeholder().is_some()
    }

    pub fn key(&self, key: &str) -> Option<Act> {
        let question = self.question()?;
        if self.typing() {
            return match key {
                "enter" => Some(Act::Send),
                "escape" => Some(Act::Back),
                _ => None,
            };
        }
        let rows = question.rows();
        let listed = !matches!(question.shape, Shape::Approval(_));
        let many = matches!(question.shape, Shape::Pick(Pick { many: true, .. }));
        match key {
            "up" if listed => Some(Act::Light(self.lit.saturating_sub(1))),
            "down" if listed => Some(Act::Light(
                self.lit.saturating_add(1).min(rows.saturating_sub(1)),
            )),
            "space" if listed => Some(Act::Pick(self.lit)),
            "enter" if many => Some(Act::Send),
            "enter" if listed => Some(Act::Pick(self.lit)),
            "e" if question.long() => Some(Act::More),
            _ => key
                .parse::<usize>()
                .ok()
                .filter(|number| (1..=rows.min(MAX_KEYS)).contains(number))
                .map(|number| Act::Pick(number - 1)),
        }
    }

    pub fn act(&mut self, act: Act, typed: &str, now: Instant) -> bool {
        let questions = self.questions.clone();
        let Some(question) = questions.get(self.said.len()) else {
            return false;
        };
        let typed = typed.trim();
        match (&question.shape, act) {
            (_, Act::Light(at)) if at < question.rows() => self.lit = at,
            (_, Act::More) => self.more = !self.more,
            (Shape::Pick(_), Act::Back) => self.typing = false,
            (Shape::Approval(_), Act::Pick(at)) if at < Answer::ALL.len() => {
                return self.answer(vec![at], String::new(), now);
            }
            (Shape::Pick(pick), Act::Pick(at)) if pick.other_at(at) => {
                self.typing = true;
                self.lit = at;
            }
            (Shape::Pick(pick), Act::Pick(at)) if at < pick.choices.len() && !pick.many => {
                return self.answer(vec![at], String::new(), now);
            }
            (Shape::Pick(pick), Act::Pick(at)) if at < pick.choices.len() => {
                self.lit = at;
                match self.picked.iter().position(|picked| *picked == at) {
                    Some(found) => {
                        self.picked.remove(found);
                    }
                    None => self.picked.push(at),
                }
            }
            (Shape::Pick(pick), Act::Send)
                if pick.many && (!self.picked.is_empty() || (self.typing && !typed.is_empty())) =>
            {
                let typed = if self.typing { typed } else { "" };
                let mut picked = std::mem::take(&mut self.picked);
                picked.sort_unstable();
                return self.answer(picked, typed.to_owned(), now);
            }
            (Shape::Pick(_) | Shape::Typed(_), Act::Send) if self.typing() && !typed.is_empty() => {
                return self.answer(Vec::new(), typed.to_owned(), now);
            }
            _ => {}
        }
        false
    }

    fn answer(&mut self, picked: Vec<usize>, typed: String, now: Instant) -> bool {
        self.said.push(Said { picked, typed });
        self.lit = 0;
        self.picked.clear();
        self.typing = false;
        self.more = false;
        self.moved = Some(now);
        true
    }
}

type OnAct = dyn Fn(Act, &mut Window, &mut App);

fn wrapping() -> Div {
    div()
        .flex()
        .flex_wrap()
        .items_center()
        .gap_x(px(ASK_GAP))
        .gap_y(px(WRAP_GAP))
        .min_w_0()
}

fn faint(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .min_w_0()
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, T3))
        .child(text.into())
}

fn keyed(
    id: ElementId,
    label: &'static str,
    key: &'static str,
    primary: bool,
    act: Act,
    on: &Rc<OnAct>,
    theme: &Theme,
) -> Stateful<Div> {
    let kind = if primary {
        ButtonKind::Primary
    } else {
        ButtonKind::Text
    };
    let mark = theme.color(ColorToken::ButtonPrimaryText);
    let key = kbd(key, theme).when(primary, |key| {
        key.bg(tint(mark, PRIMARY_KBD_FILL))
            .text_color(tint(mark, PRIMARY_KBD_TEXT))
    });
    let on = on.clone();
    button(id, label, None, kind, theme)
        .h(px(ASK_BUTTON))
        .text_size(px(FONT_TAB))
        .child(key)
        .on_click(move |_, window, cx| on(act, window, cx))
}

const NUMBERS: [&str; MAX_KEYS] = ["1", "2", "3", "4", "5", "6", "7", "8", "9"];

fn number(at: usize) -> &'static str {
    NUMBERS.get(at).copied().unwrap_or_default()
}

fn approval(id: &'static str, ask: &Ask, on: &Rc<OnAct>, theme: &Theme) -> Div {
    let buttons = Answer::ALL.iter().enumerate().map(|(at, answer)| {
        keyed(
            (id, APPROVAL_IDS + at).into(),
            answer.label(),
            number(at),
            *answer == Answer::Allow,
            Act::Pick(at),
            on,
            theme,
        )
    });
    wrapping()
        .gap_x(px(BUTTON_GAP))
        .children(buttons)
        .child(faint(ask.hint.clone(), theme).ml_auto())
}

fn approval_top(ask: &Ask, theme: &Theme) -> Div {
    wrapping()
        .text_size(px(FONT_BODY))
        .child(mark(theme))
        .child(div().flex_none().child(format!("{} wants", ask.tool)))
        .child(
            div()
                .min_w_0()
                .font_family(mono(theme))
                .text_size(px(FONT_TAB))
                .child(ask.command.clone()),
        )
        .child(faint(format!("writes {}", ask.writes), theme).ml_auto())
}

fn mark(theme: &Theme) -> Div {
    div()
        .flex_none()
        .font_weight(FontWeight::SEMIBOLD)
        .text_color(theme.color(ColorToken::StatusAccent))
        .child("?")
}

fn check(picked: bool, theme: &Theme) -> Div {
    let fill = theme.color(ColorToken::ButtonPrimary);
    let tick = theme.color(ColorToken::ButtonPrimaryText);
    div()
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(CHECK_BOX))
        .rounded(px(RADIUS_BADGE))
        .bg(tint(fill, if picked { 1.0 } else { 0.0 }))
        .shadow(vec![ring(ink(theme, if picked { 0.0 } else { BOX_RING }))])
        .transitions(|transitions| transitions.bg(Motion::new(TOGGLE_MS).with_easing(EASE_OUT)))
        .child(glyph(
            Glyph::Check,
            ICON_TINY,
            tint(tick, if picked { 1.0 } else { 0.0 }),
        ))
}

struct Row {
    at: usize,
    label: SharedString,
    about: Option<SharedString>,
    lit: bool,
    ticked: Option<bool>,
}

fn row(id: &'static str, row: Row, on: &Rc<OnAct>, theme: &Theme) -> Stateful<Div> {
    let (hover, click) = (on.clone(), on.clone());
    let at = row.at;
    div()
        .id((id, at))
        .flex()
        .items_center()
        .gap(px(ASK_GAP))
        .min_w_0()
        .px(px(ROW_PAD_X))
        .py(px(CHOICE_PAD_Y))
        .rounded(px(RADIUS_ROW))
        .cursor_pointer()
        .bg(ink(theme, if row.lit { ROW_ON } else { 0.0 }))
        .transitions(|transitions| transitions.bg(Motion::new(HOVER_MS).with_easing(EASE_OUT)))
        .on_hover(move |hovered, window, cx| {
            if *hovered {
                hover(Act::Light(at), window, cx);
            }
        })
        .on_click(move |_, window, cx| click(Act::Pick(at), window, cx))
        .child(kbd(number(at), theme))
        .children(row.ticked.map(|ticked| check(ticked, theme)))
        .child(
            wrapping()
                .flex_1()
                .child(
                    div()
                        .flex_none()
                        .text_size(px(FONT_TAB))
                        .font_weight(FontWeight::MEDIUM)
                        .child(row.label),
                )
                .children(row.about.map(|about| faint(about, theme))),
        )
}

fn field_frame(field: AnyElement, theme: &Theme) -> Div {
    div()
        .min_w_0()
        .px(px(ROW_PAD_X))
        .py(px(FIELD_PAD_Y))
        .rounded(px(RADIUS_ROW))
        .shadow(vec![ring(ink(theme, FIELD_RING))])
        .child(field)
}

fn choices(
    id: &'static str,
    asking: &Asking,
    pick: &Pick,
    field: Option<AnyElement>,
    on: &Rc<OnAct>,
    theme: &Theme,
) -> Div {
    let rows = pick.choices.iter().enumerate().map(|(at, choice)| {
        row(
            id,
            Row {
                at,
                label: choice.label.clone(),
                about: choice.about.clone(),
                lit: asking.lit == at,
                ticked: pick.many.then(|| asking.picked.contains(&at)),
            },
            on,
            theme,
        )
        .into_any_element()
    });
    let last = pick.choices.len();
    let other = pick.other.as_ref().map(|_| match field {
        Some(field) if asking.typing => field_frame(field, theme).into_any_element(),
        _ => row(
            id,
            Row {
                at: last,
                label: "Other".into(),
                about: Some("type it".into()),
                lit: asking.lit == last,
                ticked: None,
            },
            on,
            theme,
        )
        .into_any_element(),
    });
    div()
        .flex()
        .flex_col()
        .gap(px(CHOICE_GAP))
        .min_w_0()
        .children(rows)
        .children(other)
}

fn open(
    id: &'static str,
    asking: &Asking,
    question: &Question,
    field: Option<AnyElement>,
    on: &Rc<OnAct>,
    theme: &Theme,
) -> Div {
    let count = asking.questions.len();
    let step = asking.said.len() + 1;
    let top = match &question.shape {
        Shape::Approval(ask) => approval_top(ask, theme),
        Shape::Pick(_) | Shape::Typed(_) => wrapping()
            .child(mark(theme))
            .child(
                div()
                    .flex_none()
                    .text_size(px(FONT_SMALL))
                    .font_weight(FontWeight::SEMIBOLD)
                    .text_color(ink(theme, T2))
                    .child(question.header.clone()),
            )
            .when(count > 1, |top| {
                top.child(faint(format!("{step} of {count}"), theme).ml_auto())
            }),
    };
    let clamped = question.long() && !asking.more;
    let more = on.clone();
    let brighter = ink(theme, T2);
    let text = (!question.text.is_empty()).then(|| {
        div()
            .flex()
            .flex_col()
            .gap(px(WRAP_GAP))
            .min_w_0()
            .child(
                div()
                    .min_w_0()
                    .text_size(px(FONT_BODY))
                    .when(clamped, |text| {
                        text.text_ellipsis().line_clamp(CLAMPED_LINES)
                    })
                    .child(question.text.clone()),
            )
            .when(question.long(), |text| {
                text.child(
                    div()
                        .id((id, MORE_ID))
                        .flex()
                        .items_center()
                        .gap(px(BUTTON_GAP))
                        .cursor_pointer()
                        .text_size(px(FONT_SMALL))
                        .text_color(ink(theme, T3))
                        .hover(move |style| style.text_color(brighter))
                        .on_click(move |_, window, cx| more(Act::More, window, cx))
                        .child(if clamped { "more" } else { "less" })
                        .child(kbd("e", theme)),
                )
            })
    });
    let (body, hint) = match &question.shape {
        Shape::Approval(ask) => (approval(id, ask, on, theme), None),
        Shape::Pick(pick) => {
            let hint = match (pick.many, asking.typing) {
                (_, true) => "enter sends, esc goes back to the list",
                (true, false) => "a number or space ticks, enter submits",
                (false, false) => "a number, or up, down and enter",
            };
            let last = pick.rows();
            let body = choices(id, asking, pick, field, on, theme);
            let send = (pick.many || asking.typing).then(|| {
                let label = if asking.typing { "Send" } else { "Submit" };
                keyed(
                    (id, SEND_ID).into(),
                    label,
                    "enter",
                    asking.typing || !asking.picked.is_empty(),
                    Act::Send,
                    on,
                    theme,
                )
            });
            (
                body,
                Some(foot(format!("{hint} · 1 to {last}"), send, theme)),
            )
        }
        Shape::Typed(_) => {
            let send = keyed(
                (id, SEND_ID).into(),
                "Send",
                "enter",
                true,
                Act::Send,
                on,
                theme,
            );
            (
                field.map_or_else(div, |field| field_frame(field, theme)),
                Some(foot(
                    "enter sends, shift enter starts a new line".into(),
                    Some(send),
                    theme,
                )),
            )
        }
    };
    div()
        .flex()
        .flex_col()
        .gap(px(ASK_GAP))
        .min_w_0()
        .child(top)
        .children(text)
        .child(body)
        .children(hint)
}

fn foot(hint: String, send: Option<Stateful<Div>>, theme: &Theme) -> Div {
    wrapping()
        .child(faint(hint, theme))
        .children(send.map(|send| send.ml_auto()))
}

pub fn ask_bar(
    id: &'static str,
    asking: &Asking,
    field: Option<AnyElement>,
    shown: f32,
    active: bool,
    on: impl Fn(Act, &mut Window, &mut App) + 'static,
    theme: &Theme,
) -> Div {
    let on: Rc<OnAct> = Rc::new(on);
    let history = (asking.questions.len() > 1).then(|| {
        asking
            .questions
            .iter()
            .zip(&asking.said)
            .map(|(question, said)| faint(format!("· {}", question.told(said)), theme))
    });
    let now = match asking.question() {
        Some(question) => open(id, asking, question, field, &on, theme),
        None => {
            let last = match (&*asking.questions, asking.said.as_slice()) {
                ([question], [said]) => question.told(said),
                _ => format!("answered all {}, the lead goes on", asking.said.len()),
            };
            div()
                .min_h(px(ASK_BUTTON))
                .flex()
                .items_center()
                .min_w_0()
                .text_size(px(FONT_TAB))
                .text_color(ink(theme, T3))
                .child(format!("· {last}"))
        }
    };
    let edge = if active { ACTIVE_RING } else { COMPOSER_RING };
    div()
        .flex()
        .flex_col()
        .gap(px(ASK_GAP))
        .min_w_0()
        .px(px(ASK_PAD_X))
        .py(px(ASK_PAD_Y))
        .rounded(px(ASK_RADIUS))
        .bg(theme.color(ColorToken::FieldFill))
        .shadow(vec![ring(ink(theme, edge))])
        .text_color(theme.color(ColorToken::TextBase))
        .children(history.into_iter().flatten())
        .child(now.opacity(shown))
}

pub fn phase_line(
    id: impl Into<ElementId>,
    waiting: bool,
    waited: impl Into<SharedString>,
    theme: &Theme,
) -> Div {
    let phase = if waiting { "waiting on you" } else { "working" };
    div()
        .flex()
        .items_center()
        .gap(px(ASK_GAP))
        .min_w_0()
        .px(px(PHASE_PAD_X))
        .pb(px(PHASE_PAD_BOTTOM))
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, T3))
        .child(spinner(id, theme))
        .child(div().flex_none().child(phase))
        .child(div().min_w_0().child(format!("· {}", waited.into())))
}
