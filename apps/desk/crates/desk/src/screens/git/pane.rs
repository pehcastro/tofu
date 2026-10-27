use gpui::{
    AnyElement, Context, Entity, Render, StyleRefinement, Subscription, WeakEntity, Window, div,
    prelude::*, px,
};

use super::model::GitScreen;
use super::{banner, changes, header, history, sidebar};

#[derive(Clone, Copy)]
pub enum Part {
    Header,
    Banner,
    Sidebar,
    History,
    Changes,
}

impl Part {
    fn id(self) -> &'static str {
        match self {
            Part::Header => "git-header",
            Part::Banner => "git-banner",
            Part::Sidebar => "git-sidebar",
            Part::History => "git-history",
            Part::Changes => "git-changes",
        }
    }
}

pub struct Pane {
    screen: WeakEntity<GitScreen>,
    part: Part,
    _redrawn: Subscription,
}

pub struct Panes {
    header: Entity<Pane>,
    banner: Entity<Pane>,
    sidebar: Entity<Pane>,
    history: Entity<Pane>,
    changes: Entity<Pane>,
}

impl Panes {
    pub fn new(cx: &mut Context<GitScreen>) -> Panes {
        let screen = cx.entity();
        let mut pane = |part| {
            cx.new(|cx| Pane {
                screen: screen.downgrade(),
                part,
                _redrawn: cx.observe(&screen, |_, _, cx| cx.notify()),
            })
        };
        Panes {
            header: pane(Part::Header),
            banner: pane(Part::Banner),
            sidebar: pane(Part::Sidebar),
            history: pane(Part::History),
            changes: pane(Part::Changes),
        }
    }

    pub fn element(&self, part: Part) -> AnyElement {
        let full = StyleRefinement::default().size_full();
        match part {
            Part::Header => self.header.clone().cached(
                StyleRefinement::default()
                    .flex_none()
                    .w_full()
                    .h(px(header::HEIGHT)),
            ),
            Part::Banner => return self.banner.clone().into_any_element(),
            Part::Sidebar => self.sidebar.clone().cached(full),
            Part::History => self.history.clone().cached(full),
            Part::Changes => self.changes.clone().cached(full),
        }
        .into_any_element()
    }
}

impl Render for Pane {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let part = self.part;
        desk_perf::span!(part.id());
        self.screen
            .update(cx, |screen, cx| match part {
                Part::Header => header::render(screen, window, cx),
                Part::Banner => {
                    banner::render(screen, window, cx).unwrap_or_else(|| div().into_any_element())
                }
                Part::Sidebar => sidebar::render(screen, window, cx),
                Part::History => history::list(screen, cx),
                Part::Changes => changes::panel(screen, window, cx),
            })
            .unwrap_or_else(|_| div().into_any_element())
    }
}
