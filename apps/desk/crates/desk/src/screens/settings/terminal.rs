use std::path::PathBuf;

use desk_terminal::shells;
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::caption;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::paint::{glyph, ink, pressed, ring, tint};
use desk_ui::components::size::{T2, T3};
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::ICON_SMALL;
use desk_ui::theme::ColorToken;
use gpui::{
    ClickEvent, Context, PathPromptOptions, Render, SharedString, Window, div, prelude::*, px,
};

const GROUP: &str = "Shells";
const BROWSE: &str = "Browse";
const ROW_PAD_Y: f32 = 11.0;
const ROW_RULE: f32 = 0.04;
const ROW_NAME: f32 = 13.5;
const ROW_ABOUT: f32 = 12.5;
const CARD_RADIUS: f32 = 11.0;
const CARD_FILL: f32 = 0.2;
const CARD_RING: f32 = 0.05;

pub struct TerminalPage {
    found: Vec<PathBuf>,
    picked: PathBuf,
}

impl TerminalPage {
    pub(super) fn new() -> Self {
        let mut page = TerminalPage {
            found: shells::found(),
            picked: shells::current(),
        };
        page.list_picked();
        eprintln!(
            "desk: settings terminal finds {} shells, {} in use",
            page.found.len(),
            page.picked.display()
        );
        page
    }

    fn list_picked(&mut self) {
        if !self.found.contains(&self.picked) {
            self.found.push(self.picked.clone());
        }
    }

    fn pick(&mut self, shell: PathBuf, cx: &mut Context<Self>) {
        match shells::save(&shell) {
            Ok(()) => eprintln!("desk: settings terminal shell {}", shell.display()),
            Err(error) => eprintln!(
                "desk: settings terminal shell {} for this run only: {error}",
                shell.display()
            ),
        }
        self.picked = shell;
        self.list_picked();
        cx.notify();
    }

    fn browse(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let asked = cx.prompt_for_paths(PathPromptOptions {
            files: true,
            directories: false,
            multiple: false,
            prompt: Some("Pick a shell".into()),
        });
        cx.spawn_in(window, async move |page, cx| {
            let shell = match asked.await {
                Ok(Ok(Some(mut picked))) => picked.pop(),
                Ok(Ok(None)) => None,
                Ok(Err(error)) => {
                    eprintln!("desk: settings terminal: the file dialog failed: {error}");
                    None
                }
                Err(_) => {
                    eprintln!("desk: settings terminal: the file dialog closed without an answer");
                    None
                }
            };
            let Some(shell) = shell else {
                return eprintln!("desk: settings terminal: no shell was picked");
            };
            if page.update(cx, |page, cx| page.pick(shell, cx)).is_err() {
                eprintln!("desk: settings terminal closed before the pick landed");
            }
        })
        .detach();
    }
}

impl Render for TerminalPage {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let hover = theme.color(ColorToken::StateHover);
        let rows = self.found.iter().map(|shell| {
            let path = shell.display().to_string();
            let picked = shell.clone();
            pressed(
                div()
                    .id(SharedString::from(path.clone()))
                    .flex()
                    .items_center()
                    .gap_3()
                    .py(px(ROW_PAD_Y))
                    .px_4()
                    .border_b_1()
                    .border_color(ink(&theme, ROW_RULE))
                    .cursor_pointer()
                    .hover(move |style| style.bg(hover))
                    .child(
                        div()
                            .flex_1()
                            .flex()
                            .flex_col()
                            .child(div().text_size(px(ROW_NAME)).child(shells::name(shell)))
                            .child(
                                div()
                                    .text_size(px(ROW_ABOUT))
                                    .text_color(ink(&theme, T3))
                                    .child(path),
                            ),
                    )
                    .when(*shell == self.picked, |row| {
                        row.child(glyph(Glyph::Check, ICON_SMALL, ink(&theme, T2)))
                    }),
                theme.color(ColorToken::CardsInnerFill),
            )
            .on_click(cx.listener(move |page, _: &ClickEvent, _, cx| page.pick(picked.clone(), cx)))
        });
        div()
            .child(caption(GROUP, &theme).pt_2p5().pb_2())
            .child(
                div()
                    .flex()
                    .flex_col()
                    .mb_2p5()
                    .rounded(px(CARD_RADIUS))
                    .bg(tint(theme.color(ColorToken::Shadow), CARD_FILL))
                    .shadow(vec![ring(ink(&theme, CARD_RING))])
                    .children(rows),
            )
            .child(div().flex().justify_end().child(
                button("terminal-browse", BROWSE, None, ButtonKind::Plain, &theme).on_click(
                    cx.listener(|page, _: &ClickEvent, window, cx| page.browse(window, cx)),
                ),
            ))
    }
}
