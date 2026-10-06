use std::sync::Arc;

use gpui::{
    ClickEvent, Context, Div, FontWeight, Image, ImageFormat, Rgba, SharedString, Stateful, Window,
    div, img, prelude::*, px,
};

use super::Onboarding;
use super::fixture::{
    CLASSIFIERS, HINT, PROJECTS, PROVIDERS, STEPS, TELL_CHECK, TELL_FOLDER, TELL_OPEN,
};
use super::paint::{MONO, T3, glyph, hex, medium, ringed, shadow, spacer, text, tint, white};

const CLAUDE: &[u8] = include_bytes!("../../../../../assets/intro/claude.svg");
const FOLDER: &[u8] = include_bytes!("../../../../../assets/intro/folder.svg");
const FOLDER_OPEN: &[u8] = include_bytes!("../../../../../assets/intro/folder-open.svg");
const TICK: &str = r#"<path d="M3.5 8.5l3 3 6-7"/>"#;
const KEY: &str = r#"<circle cx="5.5" cy="10.5" r="2.5"/><path d="M7.5 8.5l5-5M11 5l1.5 1.5"/>"#;
const ADD: Rgba = hex(0x7fd6a6);
const INK: Rgba = white(0.9);
const SHELL_TOP: f32 = 150.0;
const SHELL_WIDTH: f32 = 780.0;
const BLURRED_HEIGHT: f32 = 634.0;
const SHELL_SHIFT: f32 = 6.0;

fn icon(bytes: &'static [u8]) -> gpui::Img {
    img(Arc::new(Image::from_bytes(
        ImageFormat::Svg,
        bytes.to_vec(),
    )))
    .flex_none()
    .size(px(16.0))
}

fn card() -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(12.0))
        .px(px(14.0))
        .py(px(11.0))
        .rounded(px(10.0))
        .bg(tint(0x000000, 0.18))
}

fn caption(label: &str) -> Div {
    text(10.0, 10.0, white(0.45), label.to_uppercase()).font_weight(FontWeight::SEMIBOLD)
}

fn button(
    id: impl Into<gpui::ElementId>,
    label: impl Into<SharedString>,
    primary: bool,
    height: f32,
) -> Stateful<Div> {
    div()
        .id(id.into())
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .h(px(height))
        .px(px(if primary { 16.0 } else { 12.0 }))
        .rounded(px(8.0))
        .cursor_pointer()
        .text_size(px(13.0))
        .font_weight(FontWeight::MEDIUM)
        .when(primary, |b| b.bg(white(0.9)).text_color(hex(0x111111)))
        .when(!primary, |b| b.bg(white(0.07)))
        .child(label.into())
}

fn two_lines(title: impl Into<SharedString>, sub: Div) -> Div {
    div()
        .flex_1()
        .flex()
        .flex_col()
        .line_height(px(19.0))
        .child(title.into())
        .child(sub)
}

fn badge(label: impl Into<SharedString>) -> Div {
    text(11.5, 16.0, hex(0x86e0b3), label)
        .px(px(9.0))
        .py(px(2.0))
        .rounded_full()
        .bg(tint(0x86e0b3, 0.12))
}

fn field(label: &'static str, ink: Rgba) -> Div {
    ringed(9.0, 0.12)
        .flex_1()
        .h(px(30.0))
        .px(px(12.0))
        .flex()
        .items_center()
        .bg(tint(0x000000, 0.3))
        .font_family(MONO)
        .text_size(px(12.5))
        .text_color(ink)
        .child(label)
}

impl Onboarding {
    fn set(
        change: impl Fn(&mut Self) + 'static,
    ) -> impl Fn(&mut Self, &ClickEvent, &mut Window, &mut Context<Self>) {
        move |this, _, _, cx| {
            change(this);
            cx.notify();
        }
    }

    fn tabs(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        div()
            .h(px(36.0))
            .flex()
            .items_end()
            .gap(px(2.0))
            .pl(px(9.0))
            .pr(px(6.0))
            .pb(px(1.0))
            .children(STEPS.iter().enumerate().map(|(index, label)| {
                let on = index == self.step;
                div()
                    .id(("step", index))
                    .h(px(30.0))
                    .pl(px(10.0))
                    .pr(px(9.0))
                    .flex()
                    .items_center()
                    .gap(px(7.0))
                    .rounded(px(8.0))
                    .cursor_pointer()
                    .text_size(px(12.5))
                    .font_weight(FontWeight::MEDIUM)
                    .text_color(white(if on { 1.0 } else { 0.5 }))
                    .when(on, |tab| tab.bg(white(0.055)))
                    .on_click(cx.listener(Self::set(move |this| {
                        if index <= this.step {
                            this.step = index;
                        }
                    })))
                    .child(text(11.0, 12.0, white(T3), (index + 1).to_string()).font_family(MONO))
                    .child(*label)
                    .when(index < self.step, |tab| {
                        tab.child(glyph(TICK, 13.0, ADD, scale))
                    })
            }))
            .child(spacer())
            .child(div().pb(px(8.0)).child(text(12.0, 12.0, white(T3), HINT)))
    }

    fn models(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        let connected = [
            self.connected.claude,
            self.connected.codex,
            self.connected.key,
        ]
        .iter()
        .filter(|on| **on)
        .count();
        let provider = PROVIDERS[self.provider];
        let sub = |label: &'static str| text(12.5, 19.0, white(T3), label);
        let subscription =
            |id: &'static str,
             mark: Div,
             name: &'static str,
             line: &'static str,
             on: bool,
             who: &'static str,
             cx: &mut Context<Self>| {
                card()
                    .child(mark)
                    .child(two_lines(name, sub(line)))
                    .when(on, |row| row.child(badge(who)))
                    .when(!on, |row| {
                        row.child(button(id, "Sign in", false, 28.0).on_click(cx.listener(
                            Self::set(move |this| {
                                if id == "claude" {
                                    this.connected.claude = true;
                                } else {
                                    this.connected.codex = true;
                                }
                            }),
                        )))
                    })
            };
        let mark = |fill: Rgba| {
            div()
                .flex()
                .flex_none()
                .items_center()
                .justify_center()
                .size(px(30.0))
                .rounded(px(9.0))
                .bg(fill)
        };
        div()
            .flex()
            .flex_col()
            .gap(px(14.0))
            .child(
                div()
                    .flex()
                    .items_end()
                    .child(text(18.0, 23.0, INK, "How do you pay for models?").font_weight(FontWeight::SEMIBOLD))
                    .child(spacer())
                    .child(text(12.5, 23.0, white(T3), format!("{connected} connected"))),
            )
            .child(caption("Subscriptions"))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .gap(px(6.0))
                    .child(subscription(
                        "claude",
                        mark(tint(0xd97757, 0.16)).child(icon(CLAUDE)),
                        "Claude",
                        "Pro or Max · signs in through your browser · claude-sub",
                        self.connected.claude,
                        "pehcastro · Max",
                        cx,
                    ))
                    .child(subscription(
                        "codex",
                        mark(white(0.08)).child(text(13.0, 13.0, INK, "C").font_weight(FontWeight::BOLD)),
                        "ChatGPT",
                        "Plus or Pro · signs in through your browser · codex-sub",
                        self.connected.codex,
                        "personal · Pro",
                        cx,
                    )),
            )
            .child(caption("Keys"))
            .child(
                card()
                    .flex_col()
                    .items_stretch()
                    .gap(px(10.0))
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .gap(px(12.0))
                            .child(mark(white(0.08)).child(glyph(KEY, 13.0, white(1.0), scale)))
                            .child(two_lines("An API key", sub("any provider, paid per token")))
                            .when(self.connected.key, |row| row.child(badge(format!("{provider} · ····3f1a"))))
                            .when(!self.connected.key, |row| {
                                row.child(button("add-key", "Add a key", false, 28.0).on_click(cx.listener(Self::set(|this| this.key_open = !this.key_open))))
                            }),
                    )
                    .when(self.key_open, |key| {
                        key.child(
                            div()
                                .flex()
                                .flex_col()
                                .gap(px(10.0))
                                .pl(px(42.0))
                                .child(
                                    div()
                                        .flex()
                                        .gap(px(2.0))
                                        .p(px(2.0))
                                        .rounded(px(9.0))
                                        .bg(white(0.05))
                                        .text_size(px(12.5))
                                        .w_auto()
                                        .self_start()
                                        .children(PROVIDERS.iter().enumerate().map(|(index, name)| {
                                            let on = index == self.provider;
                                            div()
                                                .id(("provider", index))
                                                .px(px(11.0))
                                                .py(px(4.0))
                                                .rounded(px(7.0))
                                                .cursor_pointer()
                                                .text_color(white(if on { 1.0 } else { 0.5 }))
                                                .when(on, |tab| tab.bg(white(0.12)))
                                                .on_click(cx.listener(Self::set(move |this| this.provider = index)))
                                                .child(*name)
                                        })),
                                )
                                .child(
                                    div()
                                        .flex()
                                        .gap(px(8.0))
                                        .child(field("sk-••••••••••••••••3f1a", white(1.0)))
                                        .child(button("save-key", "Check and save", true, 30.0).px(px(12.0)).on_click(cx.listener(Self::set(|this| {
                                            this.connected.key = true;
                                            this.key_open = false;
                                        })))),
                                )
                                .child(text(12.0, 16.0, white(T3), "checked with one free call, then stored in ~/.tofu/agent.db; never printed, never sent to the chat")),
                        )
                    }),
            )
    }

    fn classifiers(&self, cx: &mut Context<Self>) -> Div {
        div()
            .flex()
            .flex_col()
            .gap(px(12.0))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .child(text(18.0, 23.0, INK, "Pick a classifier").font_weight(FontWeight::SEMIBOLD))
                    .child(
                        div()
                            .mt(px(7.0))
                            .text_size(px(13.5))
                            .line_height(px(21.0))
                            .text_color(white(0.85))
                            .whitespace_nowrap()
                            .child("A small model tofu asks at fixed points: may this command run, is this output worth reading, is the work really done.")
                            .child("It answers in milliseconds, so the lead does not have to."),
                    ),
            )
            .child(
                div().flex().flex_col().gap(px(6.0)).children(CLASSIFIERS.iter().enumerate().map(|(index, choice)| {
                    let on = index == self.classifier;
                    let ring = |size: f32, fill: Rgba| div().flex().flex_none().items_center().justify_center().size(px(size)).rounded_full().bg(fill);
                    let dot = if on {
                        ring(17.0, white(1.0)).m(px(-1.5)).child(ring(14.0, hex(0x17161c)).child(ring(8.0, white(1.0))))
                    } else {
                        div().flex_none().size(px(14.0)).rounded_full().border(px(1.5)).border_color(white(0.4))
                    };
                    ringed(10.0, if on { 0.55 } else { 0.08 })
                        .flex()
                        .flex_col()
                        .bg(tint(0x000000, 0.18))
                        .when(on, |card| card.bg(white(0.06)))
                        .child(
                            div()
                                .id(("classifier", index))
                                .flex()
                                .items_center()
                                .gap(px(12.0))
                                .px(px(14.0))
                                .py(px(11.0))
                                .cursor_pointer()
                                .on_click(cx.listener(Self::set(move |this| this.classifier = index)))
                                .child(dot)
                                .child(two_lines(choice.title, text(12.5, 19.0, white(T3), choice.sub))),
                        )
                        .when(on && choice.needs_key, |card| {
                            card.child(
                                div()
                                    .flex()
                                    .gap(px(8.0))
                                    .pl(px(40.0))
                                    .pr(px(14.0))
                                    .pb(px(12.0))
                                    .child(field("paste the key", white(T3)))
                                    .child(button("check", "Check", false, 30.0).on_click(cx.listener(Self::tell(TELL_CHECK)))),
                            )
                        })
                })),
            )
    }

    fn projects(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        div()
            .flex()
            .flex_col()
            .gap(px(12.0))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .child(text(18.0, 23.0, INK, "Pick a project").font_weight(FontWeight::SEMIBOLD))
                    .child(
                        text(13.5, 23.0, white(0.85), "A folder to work in. tofu reads it for git, past sessions and rules; it never commits.")
                            .mt(px(6.0)),
                    ),
            )
            .child(
                div()
                    .flex()
                    .flex_col()
                    .gap(px(6.0))
                    .children(PROJECTS.iter().enumerate().map(|(index, project)| {
                        let on = self.project == Some(index);
                        ringed(10.0, if on { 0.55 } else { 0.08 })
                            .id(("pick", index))
                            .cursor_pointer()
                            .flex()
                            .items_center()
                            .gap(px(12.0))
                            .px(px(14.0))
                            .py(px(11.0))
                            .bg(tint(0x000000, 0.18))
                            .when(on, |card| card.bg(white(0.06)))
                            .on_click(cx.listener(Self::set(move |this| this.project = Some(index))))
                            .child(icon(FOLDER))
                            .child(two_lines(project.name, text(11.5, 19.0, white(T3), project.path).font_family(MONO)))
                    }))
                    .child(
                        ringed(10.0, 0.08)
                            .id("open-folder")
                            .cursor_pointer()
                            .flex()
                            .items_center()
                            .gap(px(12.0))
                            .px(px(14.0))
                            .py(px(11.0))
                            .line_height(px(23.0))
                            .on_click(cx.listener(Self::tell(TELL_FOLDER)))
                            .child(icon(FOLDER_OPEN))
                            .child(div().text_color(white(0.85)).child("Open another folder")),
                    ),
            )
            .children(self.project.map(|index| {
                div().flex().flex_wrap().gap(px(8.0)).children(PROJECTS[index].found.iter().map(|found| {
                    div()
                        .flex()
                        .items_center()
                        .gap(px(6.0))
                        .h(px(24.0))
                        .px(px(9.0))
                        .rounded(px(7.0))
                        .bg(white(0.07))
                        .child(glyph(TICK, 13.0, ADD, scale))
                        .child(medium(12.5, 12.5, white(0.85), *found))
                }))
            }))
    }

    fn footer(&self, cx: &mut Context<Self>) -> Div {
        let blocked = self.step == 1
            && ![
                self.connected.claude,
                self.connected.codex,
                self.connected.key,
            ]
            .contains(&true);
        div()
            .flex()
            .items_center()
            .gap(px(10.0))
            .pt(px(6.0))
            .border_t_1()
            .border_color(white(0.05))
            .child(
                button("back", "Back", false, 30.0)
                    .on_click(cx.listener(Self::set(|this| this.step -= 1))),
            )
            .when(blocked, |row| {
                row.child(text(12.5, 18.0, white(T3), "connect one to continue"))
            })
            .child(spacer())
            .when(self.step == 3, |row| {
                let name = self.project.map_or("", |index| PROJECTS[index].name);
                row.child(
                    button("open", format!("Open {name}"), true, 30.0)
                        .on_click(cx.listener(Self::tell(TELL_OPEN))),
                )
            })
            .when(!blocked && self.step < 3, |row| {
                row.child(
                    button("continue", "Continue", true, 30.0)
                        .on_click(cx.listener(Self::set(|this| this.step += 1))),
                )
            })
    }

    pub(super) fn setup(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        let body = match self.step {
            1 => self.models(scale, cx),
            2 => self.classifiers(cx),
            _ => self.projects(scale, cx),
        };
        div()
            .absolute()
            .inset_0()
            .flex()
            .justify_center()
            .items_start()
            .pt(px(SHELL_TOP))
            .pr(px(SHELL_SHIFT))
            .child(
                div()
                    .relative()
                    .rounded(px(12.0))
                    .overflow_hidden()
                    .w(px(SHELL_WIDTH))
                    .flex()
                    .flex_col()
                    .px(px(3.0))
                    .pb(px(3.0))
                    .shadow(shadow(45.0, 30.0, 0.5))
                    .child(
                        img(self.blurred.clone())
                            .absolute()
                            .top_0()
                            .left_0()
                            .w(px(SHELL_WIDTH))
                            .h(px(BLURRED_HEIGHT)),
                    )
                    .child(div().absolute().inset_0().bg(tint(0x0a090e, 0.82)))
                    .child(ringed(12.0, 0.1).absolute().inset_0())
                    .text_size(px(13.0))
                    .text_color(INK)
                    .child(self.tabs(scale, cx))
                    .child(
                        div()
                            .flex()
                            .flex_col()
                            .gap(px(18.0))
                            .min_h(px(360.0))
                            .pt(px(22.0))
                            .px(px(24.0))
                            .pb(px(16.0))
                            .rounded(px(11.0))
                            .child(body)
                            .child(div().flex_1())
                            .child(self.footer(cx)),
                    ),
            )
    }
}
