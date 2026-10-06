use desk_ui::components::paint::ink;
use desk_ui::theme::Theme;
use gpui::{
    ClickEvent, Context, Div, FontWeight, Stateful, div, prelude::*, px, relative, rgb, rgba,
};

use super::fixture::{self, ENUM, FOUND, OWN, RECENT, Source, TABLES, Table, VIEW};
use super::kit::{self, LINE, LIVE, T2, T3, WARN};
use super::{Conn, Studio, Tab};

impl Studio {
    pub(super) fn head(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let open = self.conn != Conn::Closed;
        div()
            .flex()
            .flex_none()
            .items_center()
            .gap(px(6.0))
            .h(px(28.0))
            .pl(px(9.0))
            .pr(px(6.0))
            .text_size(px(12.0))
            .line_height(relative(1.0))
            .font_weight(FontWeight::MEDIUM)
            .text_color(ink(theme, 0.55))
            .child(
                div()
                    .size(px(13.0))
                    .rounded(px(3.0))
                    .shadow(vec![kit::edge(ink(theme, 0.45), 1.3)]),
            )
            .child(kit::cap("Postgres", theme))
            .child(
                div()
                    .id("connection")
                    .ml(px(6.0))
                    .flex()
                    .items_center()
                    .gap(px(6.0))
                    .h(px(24.0))
                    .px(px(9.0))
                    .rounded(px(7.0))
                    .cursor_pointer()
                    .bg(ink(theme, if open { 0.14 } else { 0.07 }))
                    .text_color(ink(theme, 0.85))
                    .child(div().text_size(px(8.0)).text_color(rgb(LIVE)).child("●"))
                    .child(kit::mono(theme, 12.0).child("notes_dev · localhost:5432"))
                    .child(kit::chevron(theme))
                    .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                        this.conn = match this.conn {
                            Conn::Closed => Conn::List,
                            Conn::List | Conn::Url => Conn::Closed,
                        };
                        cx.notify();
                    })),
            )
            .child(div().flex_1())
            .child(
                div()
                    .font_weight(FontWeight::NORMAL)
                    .text_color(ink(theme, T3))
                    .child("plugin · from DATABASE_URL in .env"),
            )
    }

    fn table_row(
        &self,
        id: &'static str,
        table: &Table,
        on: bool,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Stateful<Div> {
        let row = div()
            .id(id)
            .flex()
            .items_center()
            .gap(px(10.0))
            .px(px(10.0))
            .py(px(6.0))
            .rounded(px(8.0))
            .cursor_pointer()
            .when(on, |row| row.bg(ink(theme, 0.08)))
            .child(
                div()
                    .flex_1()
                    .when(table.tell.is_some(), |name| name.text_color(ink(theme, T2)))
                    .child(table.name),
            )
            .children(table.rows.map(|rows| {
                kit::mono(theme, 11.5)
                    .text_color(ink(theme, T3))
                    .child(rows.to_string())
            }));
        match (table.tell, table.name) {
            (Some(tell), _) => row.on_click(cx.listener(Self::tell(tell))),
            (None, name) => {
                let tab = if name == "users" {
                    Tab::Users
                } else {
                    Tab::Notes
                };
                row.on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                    this.tab = tab;
                    cx.notify();
                }))
            }
        }
    }

    pub(super) fn tables(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let section = |label: &str, top: f32| {
            kit::cap(label, theme)
                .pt(px(top))
                .pb(px(6.0))
                .px(px(10.0))
                .h(px(top + 16.0))
        };
        div()
            .w(px(217.0))
            .flex_none()
            .flex()
            .flex_col()
            .gap(px(1.0))
            .px(px(8.0))
            .py(px(10.0))
            .border_r_1()
            .border_color(rgba(LINE))
            .text_size(px(13.5))
            .child(
                div()
                    .id("schema")
                    .flex()
                    .items_center()
                    .gap(px(8.0))
                    .h(px(28.0))
                    .px(px(10.0))
                    .mb(px(10.0))
                    .rounded(px(8.0))
                    .cursor_pointer()
                    .bg(rgba(0x0000_0038))
                    .text_size(px(12.5))
                    .text_color(ink(theme, T3))
                    .child(div().flex_1().child("public"))
                    .child(kit::chevron(theme))
                    .on_click(cx.listener(Self::tell(fixture::TELL_SCHEMA))),
            )
            .child(section("Tables", 2.0))
            .child(self.table_row("t-notes", &TABLES[0], self.tab == Tab::Notes, theme, cx))
            .child(self.table_row("t-users", &TABLES[1], self.tab == Tab::Users, theme, cx))
            .child(self.table_row("t-tags", &TABLES[2], false, theme, cx))
            .child(self.table_row("t-note-tags", &TABLES[3], false, theme, cx))
            .child(section("Views", 14.0))
            .child(self.table_row("t-view", &VIEW, false, theme, cx))
            .child(section("Enums", 14.0))
            .child(self.table_row("t-enum", &ENUM, false, theme, cx))
            .child(div().flex_1())
            .child(
                div()
                    .id("migrations")
                    .px(px(10.0))
                    .py(px(6.0))
                    .cursor_pointer()
                    .text_size(px(12.0))
                    .line_height(px(17.0))
                    .text_color(ink(theme, T3))
                    .child("7 of 8 migrations applied · migrations/")
                    .on_click(cx.listener(Self::tell(fixture::TELL_MIGRATIONS))),
            )
    }

    fn source(
        &self,
        id: &'static str,
        source: &Source,
        on: bool,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Stateful<Div> {
        let row = div()
            .id(id)
            .flex()
            .items_center()
            .gap(px(10.0))
            .px(px(10.0))
            .py(px(7.0))
            .rounded(px(8.0))
            .cursor_pointer()
            .when(on, |row| row.bg(ink(theme, 0.08)))
            .child(
                div()
                    .size(px(13.0))
                    .rounded(px(3.0))
                    .shadow(vec![kit::edge(ink(theme, 0.45), 1.3)]),
            )
            .child(
                div()
                    .flex_1()
                    .flex()
                    .flex_col()
                    .line_height(px(17.0))
                    .child(
                        div()
                            .flex()
                            .gap(px(4.0))
                            .child(source.name)
                            .child(div().text_color(ink(theme, T3)).child(source.kind)),
                    )
                    .child(
                        kit::mono(theme, 11.5)
                            .text_color(ink(theme, T3))
                            .child(source.detail),
                    ),
            )
            .child(
                div()
                    .text_size(px(11.5))
                    .text_color(if on { rgb(LIVE) } else { ink(theme, T3) })
                    .child(if on { "connected" } else { source.state }),
            );
        if on {
            row.on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                this.conn = Conn::Closed;
                cx.notify();
            }))
        } else {
            row.on_click(cx.listener(Self::tell(source.tell)))
        }
    }

    pub(super) fn connections(&self, theme: &Theme, cx: &mut Context<Self>) -> Option<Div> {
        let pop = div()
            .absolute()
            .left(px(96.0))
            .top(px(36.0))
            .w(px(482.0))
            .occlude()
            .rounded(px(12.0))
            .bg(rgba(kit::POP))
            .shadow(vec![
                kit::edge(ink(theme, 0.12), 1.0),
                kit::drop(22.0, 50.0, 0x0000_0099),
            ])
            .text_size(px(13.0))
            .flex()
            .flex_col();
        let caption =
            |label: &str, top: f32| kit::cap(label, theme).pt(px(top)).pb(px(6.0)).px(px(10.0));
        match self.conn {
            Conn::Closed => None,
            Conn::List => Some(
                pop.p(px(6.0))
                    .child(caption("Found in notes-app", 8.0))
                    .child(self.source(
                        "c-dev",
                        &Source {
                            name: "notes_dev",
                            kind: "postgres 16",
                            detail: "DATABASE_URL in .env · localhost:5432",
                            state: "connected",
                            tell: "",
                        },
                        true,
                        theme,
                        cx,
                    ))
                    .child(self.source("c-file", &FOUND[0], false, theme, cx))
                    .child(self.source("c-docker", &FOUND[1], false, theme, cx))
                    .child(caption("tofu's own data", 12.0))
                    .child(self.source("c-agent", &OWN[0], false, theme, cx))
                    .child(self.source("c-sessions", &OWN[1], false, theme, cx))
                    .child(caption("Recent", 12.0))
                    .child(self.source("c-recent", &RECENT, false, theme, cx))
                    .child(div().h(px(1.0)).m(px(6.0)).bg(ink(theme, 0.07)))
                    .child(
                        div()
                            .flex()
                            .gap(px(6.0))
                            .px(px(6.0))
                            .pt(px(4.0))
                            .pb(px(6.0))
                            .child(
                                kit::button("open-file", 30.0, theme)
                                    .flex_1()
                                    .text_size(px(13.0))
                                    .child("Open a file")
                                    .on_click(cx.listener(Self::tell(fixture::TELL_FILE))),
                            )
                            .child(
                                kit::button("open-url", 30.0, theme)
                                    .flex_1()
                                    .text_size(px(13.0))
                                    .child("Connect with a URL")
                                    .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                                        this.conn = Conn::Url;
                                        cx.notify();
                                    })),
                            ),
                    ),
            ),
            Conn::Url => Some(self.url_form(pop, theme, cx)),
        }
    }

    fn url_form(&self, pop: Div, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let set = |conn: Conn| {
            cx.listener(move |this: &mut Self, _: &ClickEvent, _, cx| {
                this.conn = conn;
                cx.notify();
            })
        };
        let field = |label: &'static str, value: Div| {
            div()
                .flex()
                .gap(px(12.0))
                .child(div().w(px(80.0)).text_color(ink(theme, T3)).child(label))
                .child(value)
        };
        let icon = |id: &'static str| {
            div()
                .id(id)
                .flex()
                .items_center()
                .justify_center()
                .size(px(24.0))
                .cursor_pointer()
        };
        pop.w(px(470.0))
            .px(px(16.0))
            .py(px(14.0))
            .gap(px(12.0))
            .child(
                div()
                    .flex()
                    .items_center()
                    .child(icon("url-back").mr(px(4.0)).text_color(ink(theme, T3)).child("‹").on_click(set(Conn::List)))
                    .child(div().flex_1().font_weight(FontWeight::SEMIBOLD).child("Connect with a URL"))
                    .child(icon("url-close").child("×").on_click(set(Conn::Closed))),
            )
            .child(
                kit::mono(theme, 13.0)
                    .px(px(10.0))
                    .py(px(8.0))
                    .rounded(px(8.0))
                    .bg(rgba(0x0000_004d))
                    .shadow(vec![kit::edge(ink(theme, 0.12), 1.0)])
                    .flex()
                    .child("postgres://app:")
                    .child(div().text_color(ink(theme, T3)).child("••••••"))
                    .child("@staging.internal:5432/notes"),
            )
            .child(
                div()
                    .flex()
                    .flex_col()
                    .gap(px(4.0))
                    .text_size(px(12.5))
                    .child(field("driver", div().child("postgres")))
                    .child(field("host", kit::mono(theme, 12.5).child("staging.internal:5432")))
                    .child(field("database", kit::mono(theme, 12.5).child("notes")))
                    .child(field("password", div().child("kept in agent.db, never shown again"))),
            )
            .child(
                div()
                    .flex()
                    .gap(px(6.0))
                    .items_center()
                    .child(div().text_size(px(12.0)).text_color(ink(theme, T3)).child("also"))
                    .child(kit::chip("mysql://", theme))
                    .child(kit::chip("libsql://", theme))
                    .child(kit::chip("file:", theme))
                    .child(kit::chip("duckdb", theme)),
            )
            .child(
                div()
                    .px(px(10.0))
                    .py(px(8.0))
                    .rounded(px(9.0))
                    .bg(rgba(0xe8c98a12))
                    .shadow(vec![kit::edge(rgba(0xe8c98a33), 1.0)])
                    .text_size(px(12.5))
                    .text_color(rgb(WARN))
                    .child("Not localhost, so it opens read only. Writes need you to unlock it for this session."),
            )
            .child(
                div()
                    .flex()
                    .justify_end()
                    .gap(px(6.0))
                    .child(kit::button("url-test", 28.0, theme).text_size(px(13.0)).child("Test").on_click(cx.listener(Self::tell(fixture::TELL_TEST))))
                    .child(kit::primary("url-connect", 28.0, theme).text_size(px(13.0)).child("Connect").on_click(cx.listener(Self::tell(fixture::TELL_CONNECT)))),
            )
    }
}
