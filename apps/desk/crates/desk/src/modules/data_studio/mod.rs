mod fixture;
mod grid;
mod kit;
mod side;

use desk_ui::components::paint::ink;
use desk_ui::live::ActiveTheme;
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Render, SharedString, Window, div, prelude::*,
    px, rgb,
};

use fixture::{BLANK_ID, NEW_ID, NEW_NOTE, NOTES, Note};

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    kit::load_fonts(cx)?;
    match board {
        None | Some("IDATA-1") => Ok(cx.new(|_| Studio::new()).into()),
        Some(other) => Err(format!("the data studio draws IDATA-1, not {other}")),
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Tab {
    Notes,
    Users,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Edit {
    Clean,
    Pending,
    Saved,
    Insert,
    Inserted,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Conn {
    Closed,
    List,
    Url,
}

pub struct Studio {
    tab: Tab,
    selected: Option<u32>,
    edit: Edit,
    filtered: bool,
    conn: Conn,
    asked: bool,
    told: Option<SharedString>,
}

impl Studio {
    fn new() -> Self {
        Studio {
            tab: Tab::Notes,
            selected: Some(BLANK_ID),
            edit: Edit::Clean,
            filtered: false,
            conn: Conn::Closed,
            asked: false,
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

    fn inserted(&self) -> bool {
        matches!(self.edit, Edit::Insert | Edit::Inserted)
    }

    fn all_notes(&self) -> Vec<&'static Note> {
        let extra = self.inserted().then_some(&NEW_NOTE);
        NOTES.iter().chain(extra).collect()
    }

    fn shown_notes(&self) -> Vec<&'static Note> {
        self.all_notes()
            .into_iter()
            .filter(|note| !self.filtered || note.title.is_empty())
            .collect()
    }

    fn title_of(&self, note: &Note) -> Option<&'static str> {
        match note.id {
            BLANK_ID if self.edit != Edit::Clean => Some(fixture::EDITED_TITLE),
            _ if note.title.is_empty() => None,
            _ => Some(note.title),
        }
    }

    fn pending(&self, id: u32) -> bool {
        (id == BLANK_ID && self.edit == Edit::Pending)
            || (id == NEW_ID && self.edit == Edit::Insert)
    }

    fn drawer_note(&self) -> Option<&'static Note> {
        let id = self.selected.filter(|_| self.tab == Tab::Notes)?;
        Some(NOTES.iter().find(|note| note.id == id).unwrap_or(&NOTES[0]))
    }
}

impl Render for Studio {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let body = div()
            .relative()
            .flex()
            .flex_col()
            .px(px(3.0))
            .pb(px(3.0))
            .rounded(px(12.0))
            .bg(rgb(kit::SHELL))
            .shadow(vec![kit::edge(ink(&theme, 0.06), 1.0)])
            .child(self.head(&theme, cx))
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .rounded(px(11.0))
                    .overflow_hidden()
                    .bg(rgb(kit::INNER))
                    .shadow(vec![kit::edge(ink(&theme, 0.03), 1.0)])
                    .child(self.tables(&theme, cx))
                    .child(self.main(&theme, cx)),
            )
            .children(self.connections(&theme, cx));
        kit::frame(
            &theme,
            body,
            self.told.clone(),
            cx.listener(|this, _: &ClickEvent, _, cx| {
                this.told = None;
                cx.notify();
            }),
        )
    }
}
