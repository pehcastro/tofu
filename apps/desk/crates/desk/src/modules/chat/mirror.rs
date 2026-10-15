use std::time::Instant;

use desk_ui::components::chat::fail;
use desk_ui::components::composer::Composer;
use desk_ui::components::form::TextArea;
use desk_ui::components::transcript::{Transcript, transcript};
use desk_ui::live::ActiveTheme;
use gpui::{
    App, AppContext, Context, Entity, IntoElement, Render, Subscription, Window, canvas, div,
    prelude::*, px,
};

use super::{
    COLUMN_TOP, CONTENT_WIDTH, Chat, DOCK_GAP, DOCK_INSET, FIELD_LINES, PLACEHOLDER, ROW_INSET,
    Spliced, items,
};

pub struct ChatMirror {
    owner: Entity<Chat>,
    transcript: Transcript,
    count: usize,
    area: Entity<TextArea>,
    opened: Option<Instant>,
    _watched: [Subscription; 2],
}

pub fn mirror(owner: Entity<Chat>, window: &mut Window, cx: &mut App) -> Entity<ChatMirror> {
    let opened = Instant::now();
    cx.new(|cx| {
        let submit =
            cx.listener(|mirror: &mut ChatMirror, text: &str, _, cx| mirror.send(text, cx));
        let area = cx.new(|cx| {
            TextArea::new(PLACEHOLDER.into(), window, cx)
                .bare()
                .max_lines(FIELD_LINES)
                .on_submit(submit)
        });
        let count = owner.read(cx).items.len();
        let watched = [
            cx.observe(&owner, |_, _, cx| cx.notify()),
            cx.subscribe(&owner, |mirror, _, spliced: &Spliced, _| {
                mirror.follow(spliced);
            }),
        ];
        ChatMirror {
            owner,
            transcript: Transcript::new(count).content_width(px(CONTENT_WIDTH)),
            count,
            area,
            opened: Some(opened),
            _watched: watched,
        }
    })
}

impl ChatMirror {
    fn follow(&mut self, spliced: &Spliced) {
        match spliced {
            Spliced::Rows { old, count, after } => {
                let fits = old.end <= self.count && self.count - old.len() + count == *after;
                if fits {
                    self.transcript.splice(old.clone(), *count);
                } else {
                    eprintln!(
                        "desk: chat mirror held {} rows, the chat {after}, so it reloads",
                        self.count
                    );
                    self.transcript.reset(*after);
                }
                self.count = *after;
            }
            Spliced::Cleared => {
                self.transcript.reset(0);
                self.count = 0;
            }
        }
    }

    fn send(&mut self, text: &str, cx: &mut Context<Self>) {
        let text = text.trim().to_owned();
        if text.is_empty() {
            return;
        }
        self.area.update(cx, |area, cx| area.clear(cx));
        eprintln!("desk: chat mirror sends through the chat");
        self.owner.update(cx, |chat, cx| chat.post(text, cx));
    }
}

impl Render for ChatMirror {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let owner = self.owner.read(cx);
        let (items, busy) = (owner.items.clone(), owner.running(cx).is_some());
        let problem = owner
            .problem
            .clone()
            .map(|problem| fail("tofu", problem, "", &theme));
        let rows = transcript(
            "chat-mirror-transcript",
            &self.transcript,
            &theme,
            move |at, _, cx| {
                let theme = ActiveTheme::theme(cx);
                items.get(at).map_or_else(
                    || div().into_any_element(),
                    |item| {
                        div()
                            .px(px(ROW_INSET))
                            .child(items::render(item, at, &[], &theme))
                            .into_any_element()
                    },
                )
            },
        );
        let stopper = self.owner.clone();
        let composer = Composer::new("chat-mirror-composer", self.area.clone())
            .content_width(px(CONTENT_WIDTH))
            .busy(busy)
            .on_send(cx.listener(|mirror, text: &str, _, cx| mirror.send(text, cx)))
            .on_stop(move |_, cx| stopper.update(cx, |chat, cx| chat.stop(cx)));
        let painted = self.opened.take().map(|opened| {
            canvas(
                |_, _, _| (),
                move |_, (), _, _| {
                    eprintln!(
                        "desk: chat mirror opened in {} ms",
                        opened.elapsed().as_millis()
                    );
                },
            )
            .absolute()
            .size_0()
        });
        div()
            .relative()
            .size_full()
            .flex()
            .flex_col()
            .pt(px(COLUMN_TOP))
            .child(div().relative().flex_1().min_h_0().child(rows))
            .child(
                div()
                    .flex_none()
                    .flex()
                    .flex_col()
                    .gap(px(DOCK_GAP))
                    .p(px(DOCK_INSET))
                    .children(problem)
                    .child(composer),
            )
            .children(painted)
    }
}
