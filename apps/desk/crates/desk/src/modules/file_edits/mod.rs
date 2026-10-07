mod chrome;
mod fixture;
mod paint;

use std::borrow::Cow;
use std::sync::Arc;

use gpui::{
    AnyView, App, AppContext, Context, Div, FontWeight, Image, ImageFormat, IntoElement, Render,
    Rgba, SharedString, Stateful, Window, div, prelude::*, px,
};

use desk_ui::components::list::{HoverVariant, RowGlide, bare_row};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::Theme;
use fixture::{
    AGENTS, Agent, CHANGES, Change, FILES, File, Kin, Lang, Mark, Op, STORIES, State, Story,
};
use paint::{
    ADD, DEL, GO, MONO, REACT, SHELL, T2, T3, WARN, black, file_icon, glyph, hex, medium, mono,
    ringed, spacer, stroked, text, tint, white,
};

const BACKDROP: &[u8] = include_bytes!("../chat/assets/backdrop.jpg");
const FONTS: [&[u8]; 6] = [
    include_bytes!("../../../../../assets/fonts/Geist-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-SemiBold.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-SemiBold.ttf"),
];

const FILE_MARK: &str = r#"<path d="M5 3h4l3 3v7H5z M9 3v3h3"/>"#;
const TRACE: &str = r#"<circle cx="8" cy="8" r="2.5"/><path d="M10.5 8v1a1.75 1.75 0 0 0 3.5 0V8a6 6 0 1 0-2.4 4.8"/>"#;
const CHECK: &str = r#"<path d="M3.5 8.5l3 3 6-7"/>"#;
const BACK: &str = r#"<path d="M10 4L6 8l4 4"/>"#;
const OPEN: &str = r#"<path d="M4 6l4 4 4-4"/>"#;
const CLOSED: &str = r#"<path d="M6 4l4 4-4 4"/>"#;

#[derive(Clone, Copy, PartialEq, Eq)]
enum Pick {
    All,
    Agent(&'static str),
    File(&'static str),
}

struct FileEdits {
    pick: Pick,
    folded: bool,
    backdrop: Arc<Image>,
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    let pick = match board {
        None | Some("IWY-8") => Pick::All,
        Some("S-WORK-6") => Pick::File(FILES[0].base),
        Some(other) => return Err(format!("file edits draws IWY-8 and S-WORK-6, not {other}")),
    };
    cx.text_system()
        .add_fonts(FONTS.iter().map(|font| Cow::Borrowed(*font)).collect())
        .map_err(|error| format!("file edits cannot load the Geist fonts: {error}"))?;
    let backdrop = Arc::new(Image::from_bytes(ImageFormat::Jpeg, BACKDROP.to_vec()));
    Ok(cx
        .new(|_| FileEdits {
            pick,
            folded: false,
            backdrop,
        })
        .into())
}

impl Render for FileEdits {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let scale = window.scale_factor();
        let body = div()
            .flex_1()
            .min_h_0()
            .flex()
            .child(self.side(scale, window, cx))
            .child(div().w(px(1.0)).h_full().bg(black(0.35)))
            .child(self.feed(scale, cx));
        let shell = ringed(12.0, white(0.06))
            .flex_1()
            .min_h_0()
            .flex()
            .flex_col()
            .px(px(3.0))
            .pb(px(3.0))
            .bg(SHELL)
            .child(
                div()
                    .h(px(28.0))
                    .flex_none()
                    .flex()
                    .items_center()
                    .gap(px(6.0))
                    .pl(px(9.0))
                    .pr(px(6.0))
                    .child(glyph(FILE_MARK, 13.0, white(0.55), scale))
                    .child(medium(12.0, 12.0, white(0.55), "File edits"))
                    .child(spacer())
                    .child(medium(12.0, 16.0, white(T3), fixture::FILE_SUM))
                    .child(mono(12.0, 16.0, ADD, fixture::ADD_SUM).font_weight(FontWeight::MEDIUM))
                    .child(mono(12.0, 16.0, DEL, fixture::DEL_SUM).font_weight(FontWeight::MEDIUM)),
            )
            .child(body);
        chrome::window(&self.backdrop, scale, shell)
    }
}

fn kin_ink(kin: Kin) -> Rgba {
    match kin {
        Kin::Go => hex(0x79c0ff),
        Kin::Ts => hex(0xb9a6ea),
        Kin::Qa => hex(0xf0a3b5),
        Kin::Py => hex(0xc7d97a),
    }
}

fn kin_of(id: &str) -> Kin {
    AGENTS
        .iter()
        .find(|agent| agent.id == id)
        .map_or(Kin::Go, |agent| agent.kin)
}

fn op_ink(op: Op) -> Rgba {
    match op {
        Op::Created => hex(0x52c68e),
        Op::Deleted => hex(0xf1737d),
        Op::Modified => hex(0xdeb04e),
    }
}

fn op_word(op: Op) -> &'static str {
    match op {
        Op::Created => "created",
        Op::Deleted => "deleted",
        Op::Modified => "modified",
    }
}

fn initial(id: &str) -> String {
    id.chars()
        .next()
        .map(|c| c.to_ascii_uppercase())
        .unwrap_or_default()
        .to_string()
}

fn avatar(id: &str, size: f32, letter: f32) -> Div {
    let ink = kin_ink(kin_of(id));
    div()
        .relative()
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(size))
        .rounded(px(size / 2.0))
        .bg(Rgba { alpha: 0.16, ..ink })
        .child(text(letter, letter + 2.0, ink, initial(id)).font_weight(FontWeight::SEMIBOLD))
}

fn badge(state: State, scale: f32) -> Div {
    let dot = |size: f32, inset: f32, color: Rgba| {
        div()
            .absolute()
            .right(px(inset))
            .bottom(px(inset))
            .flex()
            .items_center()
            .justify_center()
            .size(px(size + 4.0))
            .rounded(px(size / 2.0 + 2.0))
            .border_2()
            .border_color(hex(0x17161c))
            .bg(color)
    };
    match state {
        State::Running => div()
            .absolute()
            .left(px(-3.0))
            .top(px(-3.0))
            .size(px(22.0))
            .rounded(px(11.0))
            .border(px(1.5))
            .border_color(tint(0x86e0b3, 0.18)),
        State::Waiting => dot(9.0, -4.0, WARN),
        State::Failed => dot(9.0, -4.0, hex(0xf1737d)),
        State::Done => {
            dot(12.0, -5.0, hex(0x52c68e)).child(stroked(CHECK, 8.0, hex(0x0d1a12), 2.6, scale))
        }
    }
}

fn cap(label: &'static str) -> Div {
    text(10.0, 10.0, white(0.45), label.to_uppercase()).font_weight(FontWeight::SEMIBOLD)
}

fn numbers(add: &'static str, del: &'static str) -> [Div; 2] {
    [(add, ADD), (del, DEL)].map(|(body, ink)| {
        div()
            .w(px(38.0))
            .flex()
            .justify_end()
            .child(mono(11.5, 23.0, ink, body))
    })
}

fn row(
    glide: &RowGlide,
    ix: usize,
    id: impl Into<SharedString>,
    on: bool,
    height: f32,
    theme: &Theme,
) -> Stateful<Div> {
    glide.row(
        ix,
        bare_row(gpui::ElementId::Name(id.into()), false, false, theme)
            .gap(px(8.0))
            .h(px(height))
            .px(px(10.0))
            .py_0()
            .rounded(px(9.0))
            .when(on, |row| row.bg(white(0.07))),
    )
}

impl FileEdits {
    fn side(&self, scale: f32, window: &mut Window, cx: &mut Context<Self>) -> Stateful<Div> {
        let theme = ActiveTheme::theme(cx);
        let glide = RowGlide::new(
            "file-edits-side",
            HoverVariant::default(),
            &theme,
            window,
            cx,
        );
        let pick = |to: Pick| {
            cx.listener(move |this: &mut Self, _: &gpui::ClickEvent, _, cx| {
                this.pick = to;
                cx.notify();
            })
        };
        let agent_row = |(ix, agent): (usize, &Agent)| {
            let on = self.pick == Pick::Agent(agent.id);
            let [add, del] = numbers(agent.add, agent.del);
            row(&glide, 1 + ix, agent.id, on, 32.0, &theme)
                .on_click(pick(Pick::Agent(agent.id)))
                .child(
                    div()
                        .w(px(22.0))
                        .flex_none()
                        .child(avatar(agent.id, 16.0, 8.0).child(badge(agent.state, scale))),
                )
                .child(text(13.0, 23.0, kin_ink(agent.kin), agent.id))
                .child(spacer())
                .child(add)
                .child(del)
        };
        let file_row = |(ix, file): (usize, &File)| {
            let on = self.pick == Pick::File(file.base);
            let [add, del] = numbers(file.add, file.del);
            let icon = match file.lang {
                Lang::Go => GO,
                Lang::React => REACT,
            };
            row(&glide, 1 + AGENTS.len() + ix, file.base, on, 30.0, &theme)
                .on_click(pick(Pick::File(file.base)))
                .child(file_icon(icon, 14.0, scale))
                .child(text(13.0, 23.0, op_ink(file.op), file.base))
                .child(text(11.5, 23.0, white(T3), file.dir).ml(px(-4.0)))
                .child(spacer())
                .child(add)
                .child(del)
        };
        let list = div()
            .child(
                div()
                    .flex()
                    .px(px(10.0))
                    .py(px(6.0))
                    .child(cap("Sub-agents").flex_1())
                    .child(cap(fixture::RUNNING)),
            )
            .child(
                row(&glide, 0, "all", self.pick == Pick::All, 32.0, &theme)
                    .on_click(pick(Pick::All))
                    .child(
                        text(13.0, 23.0, white(0.9), "All changes")
                            .font_weight(FontWeight::SEMIBOLD),
                    )
                    .child(spacer())
                    .child(text(12.0, 23.0, white(T3), fixture::FILE_SUM)),
            )
            .children(AGENTS.iter().enumerate().map(agent_row))
            .child(
                div()
                    .px(px(10.0))
                    .pt(px(18.0))
                    .pb(px(6.0))
                    .child(cap("Files in this session")),
            )
            .children(FILES.iter().enumerate().map(file_row));
        glide
            .frame("file-edits-side-frame", list)
            .w(px(340.0))
            .flex_none()
            .h_full()
            .overflow_hidden()
            .px(px(6.0))
            .py(px(8.0))
    }

    fn feed(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        let story = STORIES
            .iter()
            .find(|story| self.pick == Pick::File(story.file));
        let file = FILES.iter().find(|file| self.pick == Pick::File(file.base));
        if let (Some(story), Some(file)) = (story, file) {
            return self.story(story, file, scale, cx);
        }
        let title: SharedString = match self.pick {
            Pick::All => "Every change in this session".into(),
            Pick::Agent(id) => format!("Changes by {id}").into(),
            Pick::File(base) => format!("History of {base}").into(),
        };
        let shown: Vec<&Change> = CHANGES
            .iter()
            .filter(|change| match self.pick {
                Pick::All => true,
                Pick::Agent(id) => change.by == id,
                Pick::File(base) => change.file == base,
            })
            .collect();
        let back = cx.listener(|this: &mut Self, _: &gpui::ClickEvent, _, cx| {
            this.pick = Pick::All;
            cx.notify();
        });
        div()
            .flex_1()
            .min_w_0()
            .h_full()
            .overflow_hidden()
            .px(px(28.0))
            .child(
                div()
                    .flex()
                    .items_end()
                    .gap(px(10.0))
                    .pt(px(18.0))
                    .pb(px(6.0))
                    .when(self.pick != Pick::All, |head| {
                        head.child(
                            div()
                                .id("back")
                                .cursor_pointer()
                                .on_click(back)
                                .child(glyph(BACK, 16.0, white(0.6), scale)),
                        )
                    })
                    .child(text(16.0, 23.0, white(0.9), title).font_weight(FontWeight::SEMIBOLD))
                    .child(text(12.5, 23.0, white(T3), "newest first")),
            )
            .when(shown.is_empty(), |feed| {
                feed.child(text(13.0, 23.0, white(T3), "No changes yet").py(px(10.0)))
            })
            .children(
                shown
                    .into_iter()
                    .map(|change| self.entry(change, scale, cx)),
            )
    }

    fn story(&self, story: &Story, file: &File, scale: f32, cx: &mut Context<Self>) -> Div {
        let back = cx.listener(|this: &mut Self, _: &gpui::ClickEvent, _, cx| {
            this.pick = Pick::All;
            cx.notify();
        });
        let fold = cx.listener(|this: &mut Self, _: &gpui::ClickEvent, _, cx| {
            this.folded = !this.folded;
            cx.notify();
        });
        let icon = match file.lang {
            Lang::Go => GO,
            Lang::React => REACT,
        };
        let button = |id: &'static str, label: &'static str| {
            div()
                .id(id)
                .h(px(28.0))
                .px(px(12.0))
                .flex()
                .items_center()
                .rounded(px(8.0))
                .bg(white(0.07))
                .cursor_pointer()
                .child(medium(12.5, 12.5, white(0.9), label))
        };
        let chevron = if self.folded { CLOSED } else { OPEN };
        div()
            .flex_1()
            .min_w_0()
            .h_full()
            .overflow_hidden()
            .px(px(28.0))
            .pt(px(16.0))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(10.0))
                    .child(
                        div()
                            .id("back")
                            .size(px(26.0))
                            .flex()
                            .items_center()
                            .justify_center()
                            .cursor_pointer()
                            .on_click(back)
                            .child(glyph(BACK, 13.0, white(0.45), scale)),
                    )
                    .child(file_icon(icon, 18.0, scale))
                    .child(
                        text(17.0, 17.0, op_ink(file.op), file.base)
                            .font_weight(FontWeight::SEMIBOLD),
                    )
                    .child(text(13.0, 17.0, white(T3), file.dir))
                    .child(spacer())
                    .child(mono(12.5, 17.0, ADD, file.add))
                    .child(mono(12.5, 17.0, DEL, file.del))
                    .child(glyph(
                        TRACE,
                        13.0,
                        Rgba {
                            alpha: 0.7,
                            ..kin_ink(Kin::Ts)
                        },
                        scale,
                    ))
                    .child(button("open-in-editor", "Open in editor"))
                    .child(button("undo", "Undo")),
            )
            .child(
                text(12.5, 20.0, white(T3), story.note)
                    .mt(px(6.0))
                    .mb(px(18.0))
                    .ml(px(36.0)),
            )
            .child(
                div()
                    .id("edit")
                    .flex()
                    .items_center()
                    .gap(px(9.0))
                    .py(px(6.0))
                    .cursor_pointer()
                    .on_click(fold)
                    .child(glyph(chevron, 13.0, white(T3), scale))
                    .child(avatar(story.by, 22.0, 11.0))
                    .child(
                        text(13.0, 20.0, kin_ink(kin_of(story.by)), story.by)
                            .font_weight(FontWeight::SEMIBOLD),
                    )
                    .child(text(
                        13.0,
                        20.0,
                        white(T3),
                        format!("edited \u{b7} {}", story.ago),
                    ))
                    .child(spacer())
                    .child(text(12.0, 20.0, white(T3), story.summary)),
            )
            .when(!self.folded, |view| {
                view.child(
                    ringed(10.0, white(0.06))
                        .mt(px(4.0))
                        .ml(px(22.0))
                        .bg(black(0.26))
                        .overflow_hidden()
                        .child(div().px(px(12.0)).py(px(6.0)).bg(white(0.024)).child(mono(
                            11.5,
                            23.0,
                            white(T3),
                            story.head,
                        )))
                        .child(div().py(px(4.0)).children(story.added.iter().zip(1..).map(
                            |(code, number)| {
                                div()
                                    .h(px(20.0))
                                    .flex()
                                    .bg(tint(0x52c68e, 0.1))
                                    .child(
                                        div().w(px(54.0)).pr(px(12.0)).flex().justify_end().child(
                                            mono(12.0, 20.0, white(0.22), number.to_string()),
                                        ),
                                    )
                                    .child(div().w(px(14.0)).child(mono(
                                        12.0,
                                        20.0,
                                        tint(0xc6ecd6, 0.7),
                                        "+",
                                    )))
                                    .child(
                                        mono(12.0, 20.0, hex(0xc6ecd6), *code).whitespace_nowrap(),
                                    )
                            },
                        ))),
                )
            })
    }

    fn entry(&self, change: &'static Change, scale: f32, cx: &mut Context<Self>) -> Div {
        let open_file = cx.listener(move |this: &mut Self, _: &gpui::ClickEvent, _, cx| {
            this.pick = Pick::File(change.file);
            cx.notify();
        });
        div()
            .flex()
            .gap(px(12.0))
            .py(px(10.0))
            .child(avatar(change.by, 18.0, 9.0))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .flex()
                    .flex_col()
                    .child(
                        div()
                            .h(px(20.0))
                            .flex()
                            .items_center()
                            .gap(px(8.0))
                            .text_size(px(13.5))
                            .line_height(px(20.0))
                            .child(
                                div()
                                    .flex()
                                    .items_center()
                                    .gap(px(10.0))
                                    .child(
                                        text(13.5, 20.0, kin_ink(kin_of(change.by)), change.by)
                                            .font_weight(FontWeight::SEMIBOLD),
                                    )
                                    .child(text(13.5, 20.0, white(T3), op_word(change.op)))
                                    .child(
                                        div()
                                            .id(SharedString::from(format!("feed-{}", change.file)))
                                            .cursor_pointer()
                                            .on_click(open_file)
                                            .child(medium(
                                                13.5,
                                                20.0,
                                                op_ink(change.op),
                                                change.file,
                                            )),
                                    )
                                    .child(text(12.0, 20.0, white(T3), change.dir)),
                            )
                            .child(spacer())
                            .child(mono(11.5, 20.0, ADD, change.add))
                            .child(mono(11.5, 20.0, DEL, change.del))
                            .child(text(11.5, 20.0, white(T3), change.ago))
                            .child(glyph(TRACE, 13.0, white(T3), scale)),
                    )
                    .child(text(13.0, 23.0, white(T2), change.summary).mt(px(3.0)))
                    .child(card(change)),
            )
    }
}

fn card(change: &Change) -> Div {
    ringed(10.0, white(0.06))
        .mt(px(6.0))
        .bg(black(0.26))
        .overflow_hidden()
        .flex()
        .flex_col()
        .child(
            div()
                .h(px(35.0))
                .flex()
                .items_center()
                .px(px(12.0))
                .bg(white(0.024))
                .rounded_t(px(10.0))
                .child(mono(11.5, 15.0, white(T3), change.hunk)),
        )
        .child(
            div()
                .py(px(4.0))
                .flex()
                .flex_col()
                .children(change.lines.iter().map(|line| {
                    let (fill, ink, sign) = match line.mark {
                        Mark::Same => (None, white(0.5), ""),
                        Mark::Gone => (Some(tint(0xf1737d, 0.1)), hex(0xf5c0c4), "-"),
                        Mark::New => (Some(tint(0x52c68e, 0.1)), hex(0xc6ecd6), "+"),
                    };
                    div()
                        .h(px(20.0))
                        .flex()
                        .items_center()
                        .font_family(MONO)
                        .when_some(fill, |row, fill| row.bg(fill))
                        .child(div().w(px(42.0)).flex().justify_end().child(mono(
                            12.0,
                            20.0,
                            white(0.22),
                            line.number,
                        )))
                        .child(
                            div()
                                .w(px(14.0))
                                .ml(px(12.0))
                                .child(mono(12.0, 20.0, ink, sign)),
                        )
                        .child(mono(12.0, 20.0, ink, line.code).whitespace_nowrap())
                })),
        )
}
