mod agents;
mod fixture;
pub(super) mod kit;
mod rules;
mod skills;

use desk_ui::components::paint::{ink, tint};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Div, ElementId, Render, Rgba, SharedString,
    Stateful, Window, div, prelude::*, px,
};

use fixture::{AGENTS, Mode, RULES, SKILLS, Tier};
use kit::{BASE, T2, frame, headline};

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    kit::load_fonts(cx)?;
    let tab = match board {
        None | Some("ILIB-1") => Tab::Rules,
        Some("ILIB-2") => Tab::Skills,
        Some("ILIB-3") => Tab::Agents,
        Some(other) => {
            return Err(format!(
                "the library screen draws ILIB-1, ILIB-2 and ILIB-3, not {other}"
            ));
        }
    };
    Ok(cx.new(|_| Library::new(tab)).into())
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Tab {
    Rules,
    Skills,
    Agents,
}

impl Tab {
    const ALL: [Tab; 3] = [Tab::Rules, Tab::Skills, Tab::Agents];

    fn name(self) -> &'static str {
        match self {
            Tab::Rules => "Rules",
            Tab::Skills => "Skills",
            Tab::Agents => "Agents",
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Filter {
    All,
    Fired,
    Overridden,
}

pub struct Library {
    tab: Tab,
    filter: Filter,
    rule: usize,
    modes: [Option<Mode>; RULES.len()],
    skill: usize,
    source: bool,
    ran: bool,
    hidden: [bool; SKILLS.len()],
    agent: usize,
    tiers: [Option<Tier>; AGENTS.len()],
    told: Option<SharedString>,
}

const TAB_TEXT: f32 = 12.5;
const TAB_RADIUS: f32 = 9.0;
const TAB_ITEM_RADIUS: f32 = 7.0;
const TAB_FILL: f32 = 0.05;
const TAB_MARGIN: f32 = 8.0;
const SEGMENT_ON: f32 = 0.12;
const SEGMENT_OFF: f32 = 0.5;

impl Library {
    fn new(tab: Tab) -> Self {
        Library {
            tab,
            filter: Filter::All,
            rule: 0,
            modes: [None; RULES.len()],
            skill: 0,
            source: false,
            ran: false,
            hidden: [false; SKILLS.len()],
            agent: 0,
            tiers: [None; AGENTS.len()],
            told: None,
        }
    }

    fn tell(
        message: &'static str,
    ) -> impl Fn(&mut Self, &ClickEvent, &mut Window, &mut Context<Self>) {
        move |this, _, _, cx| {
            this.told = Some(message.into());
            cx.notify();
        }
    }

    fn tabs(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        group(TAB_FILL, TAB_RADIUS, theme)
            .ml(px(TAB_MARGIN))
            .text_size(px(TAB_TEXT))
            .children(Tab::ALL.into_iter().map(|tab| {
                let on = tab == self.tab;
                div()
                    .id(tab.name())
                    .py_1()
                    .px(px(11.0))
                    .rounded(px(TAB_ITEM_RADIUS))
                    .cursor_pointer()
                    .text_color(ink(theme, if on { BASE } else { T2 }))
                    .when(on, |item| item.bg(ink(theme, SEGMENT_ON)))
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        this.tab = tab;
                        cx.notify();
                    }))
                    .child(tab.name())
            }))
    }
}

fn group(fill: f32, radius: f32, theme: &Theme) -> Div {
    div()
        .flex()
        .flex_none()
        .gap(px(2.0))
        .p(px(2.0))
        .rounded(px(radius))
        .bg(ink(theme, fill))
}

fn segment(
    id: impl Into<ElementId>,
    label: &'static str,
    on: bool,
    theme: &Theme,
) -> Stateful<Div> {
    let item = div().id(id).cursor_pointer().child(label);
    if on {
        item.bg(ink(theme, SEGMENT_ON))
            .text_color(theme.color(ColorToken::TextStrong))
    } else {
        item.text_color(ink(theme, SEGMENT_OFF))
    }
}

#[derive(Clone, Copy)]
enum Look {
    Shadow,
    Enforced,
    Off,
    Hushed,
    Live,
    Warn,
}

impl Look {
    fn colors(self, theme: &Theme) -> (Rgba, Rgba) {
        match self {
            Look::Shadow => (
                tint(theme.color(ColorToken::Trace), 0.14),
                theme.color(ColorToken::MentionText),
            ),
            Look::Enforced => (ink(theme, SEGMENT_ON), theme.color(ColorToken::TextStrong)),
            Look::Off => (ink(theme, 0.05), ink(theme, 0.4)),
            Look::Hushed => (ink(theme, 0.06), ink(theme, SEGMENT_OFF)),
            Look::Live => {
                let live = theme.color(ColorToken::StatusLive);
                (tint(live, 0.12), live)
            }
            Look::Warn => {
                let warn = theme.color(ColorToken::StatusWarn);
                (tint(warn, 0.12), warn)
            }
        }
    }
}

impl Mode {
    fn look(self) -> Look {
        match self {
            Mode::Shadow => Look::Shadow,
            Mode::Enforced => Look::Enforced,
            Mode::Off => Look::Off,
        }
    }
}

impl Render for Library {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let top = headline("Library")
            .child(self.tabs(&theme, cx))
            .child(div().flex_1());
        let (top, content) = match self.tab {
            Tab::Rules => self.rules(top, &theme, cx),
            Tab::Skills => self.skills(top, &theme, cx),
            Tab::Agents => self.agents(top, &theme, cx),
        };
        frame(
            &theme,
            div().child(top).child(content),
            self.told.clone(),
            cx.listener(|this, _: &ClickEvent, _, cx| {
                this.told = None;
                cx.notify();
            }),
        )
    }
}
