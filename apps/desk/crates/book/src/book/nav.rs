use std::time::Instant;

use desk_ui::components::card::caption;
use desk_ui::components::paint::ink;
use desk_ui::components::scroll::ScrollArea;
use desk_ui::components::size::{RADIUS_ROW, T1, T2, T3};
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::SIDEBAR_WIDTH;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    ClickEvent, Context, Div, Entity, Stateful, StyleRefinement, WeakEntity, Window, div,
    prelude::*, px,
};

use super::Book;
use crate::catalog::{Group, Page, UNBUILT};

pub(super) struct Nav {
    shown: Page,
    book: WeakEntity<Book>,
}

impl Nav {
    pub(super) fn new(shown: Page, cx: &mut Context<Book>) -> Entity<Nav> {
        let book = cx.weak_entity();
        cx.new(|_| Nav { shown, book })
    }

    pub(super) fn cached(nav: &Entity<Nav>) -> impl IntoElement {
        nav.clone().cached(
            StyleRefinement::default()
                .w(px(SIDEBAR_WIDTH))
                .flex_none()
                .h_full(),
        )
    }

    pub(super) fn show(&mut self, page: Page, cx: &mut Context<Nav>) {
        self.shown = page;
        cx.notify();
    }

    fn entry(&self, page: Page, theme: &Theme) -> Stateful<Div> {
        let chosen = page == self.shown;
        let hover = theme.color(ColorToken::StateHover);
        let book = self.book.clone();
        div()
            .id(page.sheet().slug)
            .px_2()
            .py_1()
            .rounded(px(RADIUS_ROW))
            .cursor_pointer()
            .when(chosen, |entry| {
                entry
                    .bg(theme.color(ColorToken::StateActive))
                    .text_color(ink(theme, T1))
            })
            .when(!chosen, |entry| {
                entry
                    .text_color(ink(theme, T2))
                    .hover(move |style| style.bg(hover))
            })
            .child(page.sheet().title)
            .on_click(move |_: &ClickEvent, _, cx| {
                if let Some(book) = book.upgrade() {
                    book.update(cx, |book, cx| book.open(page, Instant::now(), cx));
                }
            })
    }
}

impl Render for Nav {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let groups = Group::ALL.map(|group| {
            div()
                .flex()
                .flex_col()
                .gap_0p5()
                .pt_3()
                .child(div().px_2().pb_1().child(caption(group.title(), &theme)))
                .children(
                    Page::ALL
                        .into_iter()
                        .filter(|page| page.sheet().group == group)
                        .map(|page| self.entry(page, &theme)),
                )
                .children(
                    UNBUILT
                        .iter()
                        .filter(|(of, _)| *of == group)
                        .map(|(_, name)| {
                            div()
                                .px_2()
                                .py_1()
                                .text_color(ink(&theme, T3))
                                .child(format!("{name}, not built"))
                        }),
                )
        });
        div()
            .size_full()
            .flex()
            .flex_col()
            .child(ScrollArea::new("sidebar").child(div().p_2().children(groups)))
    }
}
