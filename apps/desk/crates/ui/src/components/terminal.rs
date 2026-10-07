use desk_motion::tokens::{EASE_OUT, HOVER_MS};
use gpui::{
    Animation, AnimationExt, AnyElement, App, ClickEvent, ElementId, SharedString, Window, div,
    prelude::*, px,
};

use crate::components::button::{ButtonKind, button};
use crate::components::card::{Header, inner_card, shell};
use crate::components::chip::{badge, mono};
use crate::components::glyph::Glyph;
use crate::components::paint::{ink, tint};
use crate::components::size::{CAPTION_TEXT, FONT_SMALL, T2};
use crate::components::term::exit_color;
use crate::live::ActiveTheme;

const EXIT_FILL: f32 = 0.14;
const DIMMED: f32 = 0.35;
const NOTICE_GAP: f32 = 10.0;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TerminalState {
    Running,
    Exited { code: i32 },
}

type OnRestart = Box<dyn Fn(&ClickEvent, &mut Window, &mut App)>;

#[derive(IntoElement)]
pub struct TerminalTile {
    id: SharedString,
    title: SharedString,
    cwd: SharedString,
    state: TerminalState,
    body: AnyElement,
    restart: Option<OnRestart>,
}

impl TerminalTile {
    pub fn new(
        id: impl Into<SharedString>,
        title: impl Into<SharedString>,
        cwd: impl Into<SharedString>,
        state: TerminalState,
        body: impl IntoElement,
    ) -> Self {
        TerminalTile {
            id: id.into(),
            title: title.into(),
            cwd: cwd.into(),
            state,
            body: body.into_any_element(),
            restart: None,
        }
    }

    pub fn on_restart(
        mut self,
        restart: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
    ) -> Self {
        self.restart = Some(Box::new(restart));
        self
    }
}

impl RenderOnce for TerminalTile {
    fn render(self, _: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let id = self.id;
        let cwd = div()
            .min_w_0()
            .truncate()
            .font_family(mono(&theme))
            .text_size(px(FONT_SMALL))
            .text_color(ink(&theme, CAPTION_TEXT))
            .child(self.cwd);
        let screen = div().size_full().child(self.body);
        let screen = match self.state {
            TerminalState::Running => screen.into_any_element(),
            TerminalState::Exited { code } => {
                let color = exit_color(code, &theme);
                let notice = div()
                    .absolute()
                    .inset_0()
                    .flex()
                    .flex_col()
                    .items_center()
                    .justify_center()
                    .gap(px(NOTICE_GAP))
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .gap_2()
                            .text_color(ink(&theme, T2))
                            .child("The shell exited")
                            .child(
                                badge(format!("exit {code}"), &theme)
                                    .bg(tint(color, EXIT_FILL))
                                    .text_color(color),
                            ),
                    )
                    .children(self.restart.map(|restart| {
                        button(
                            ElementId::Name(format!("{id}-restart").into()),
                            "Restart",
                            Some(Glyph::Terminal),
                            ButtonKind::Plain,
                            &theme,
                        )
                        .on_click(restart)
                    }))
                    .with_animation(
                        ElementId::Name(format!("{id}-notice").into()),
                        Animation::new(HOVER_MS).with_easing(EASE_OUT),
                        |notice, t| notice.opacity(t),
                    );
                div()
                    .relative()
                    .size_full()
                    .child(screen.with_animation(
                        ElementId::Name(format!("{id}-dim").into()),
                        Animation::new(HOVER_MS).with_easing(EASE_OUT),
                        |screen, t| screen.opacity(1.0 - (1.0 - DIMMED) * t),
                    ))
                    .child(notice)
                    .into_any_element()
            }
        };
        shell(
            Header::Title(
                Some(Glyph::Terminal),
                self.title,
                Some(cwd.into_any_element()),
            ),
            &theme,
        )
        .size_full()
        .child(inner_card(&theme).child(screen))
    }
}
