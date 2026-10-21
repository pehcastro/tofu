use std::time::{Duration, Instant};

use desk_motion::tokens::{PANEL_IN_MS, PANEL_OUT_MS};
use desk_motion::{FlipTracker, Presence, reduced_motion};
use desk_ui::components::avatar::{Agent, AgentKind, AgentStatus, AvatarSize, avatar};
use desk_ui::components::board::{ColumnHeader, board, board_card, board_column};
use desk_ui::components::breadcrumb::{OnCrumb, breadcrumb};
use desk_ui::components::chip::code;
use desk_ui::components::dot_chip::{dot_chip, icon_chip};
use desk_ui::components::flip::flip;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::issue_row::{group_header, issue_row};
use desk_ui::components::nav_sidebar::NavSidebar;
use desk_ui::components::overlay::{actions, menu};
use desk_ui::components::paint::glyph;
use desk_ui::components::pill_tabs::PillTabs;
use desk_ui::components::priority_icon::{Priority, priority_icon};
use desk_ui::components::properties::properties_panel;
use desk_ui::components::scroll::ScrollArea;
use desk_ui::components::status_icon::{TicketStatus, status_icon};
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::metrics::{ICON, ICON_SMALL, ICON_TINY};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, ClickEvent, Context, Div, Entity, SharedString, Window, canvas, deferred, div,
    prelude::*, px, rgb_to_hsla,
};

use super::Book;
use super::kit::{block, label, named, present, spread, toggle};
use crate::catalog::Page;

const BOARD_HEIGHT: f32 = 540.0;
const LIST_HEIGHT: f32 = 540.0;
const NAV_HEIGHT: f32 = 600.0;
const FRAME_SAMPLES: usize = 60;
const STATUS_SIZES: [f32; 4] = [12.0, 14.0, 16.0, 24.0];
const SPREAD: [(TicketStatus, usize); 9] = [
    (TicketStatus::Triage, 6),
    (TicketStatus::Backlog, 40),
    (TicketStatus::Todo, 31),
    (TicketStatus::Doing, 12),
    (TicketStatus::Review, 6),
    (TicketStatus::Done, 45),
    (TicketStatus::Blocked, 4),
    (TicketStatus::NeedsDecision, 3),
    (TicketStatus::Dropped, 3),
];
const TITLES: [&str; 12] = [
    "Board columns scroll on their own while the row scrolls sideways",
    "Status icons for all nine boardy states",
    "Catch up lists open questions with their recommendation",
    "Move a card and let it glide to its new column",
    "Need attention counts blocked, review and triage",
    "Ticket page renders the acceptance revisions",
    "Composer posts a boardy log line",
    "Sidebar shows the five newest tickets with a running dot",
    "Words follow boardFlow: issue, team, project and cycle",
    "Refetch the board on boardy.changed",
    "Empty state offers Create a board",
    "Properties panel lists owns as code",
];
const LABELS: [(&str, ColorToken); 4] = [
    ("Bug", ColorToken::StatusDanger),
    ("Feature", ColorToken::AnsiMagenta),
    ("Docs", ColorToken::AnsiBlue),
    ("Perf", ColorToken::AnsiCyan),
];
const LONG_LABEL: &str = "A label that runs well past the chip's widest size";
const PICKER_DROP: f32 = 24.0;
const ASSIGNEES: [(AgentKind, u32); 4] = [
    (AgentKind::GoDev, 2),
    (AgentKind::GoDev, 3),
    (AgentKind::RustDev, 1),
    (AgentKind::Qa, 1),
];
const EPICS: [&str; 3] = ["Boardy in the desk", "Motion", "Sessions"];
const MONTHS: [&str; 4] = ["Jul", "Aug", "Sep", "Oct"];
const GLYPHS: [(Glyph, &str); 10] = [
    (Glyph::Inbox, "inbox"),
    (Glyph::Filter, "filter"),
    (Glyph::Display, "display"),
    (Glyph::Ellipsis, "ellipsis"),
    (Glyph::Label, "label"),
    (Glyph::Milestone, "milestone"),
    (Glyph::Sprint, "sprint"),
    (Glyph::Epic, "epic"),
    (Glyph::Attention, "attention"),
    (Glyph::Catchup, "catchup"),
];

struct Ticket {
    key: SharedString,
    title: SharedString,
    status: TicketStatus,
    priority: Priority,
    label: (&'static str, ColorToken),
    epic: Option<&'static str>,
    agent: AgentKind,
    age: SharedString,
    date: SharedString,
}

fn fixture() -> Vec<Ticket> {
    SPREAD
        .iter()
        .flat_map(|&(status, count)| std::iter::repeat_n(status, count))
        .enumerate()
        .map(|(n, status)| Ticket {
            key: format!("DESK-{}", 200 + n).into(),
            title: TITLES[n % TITLES.len()].into(),
            status,
            priority: Priority::ALL[n % Priority::ALL.len()],
            label: LABELS[n % LABELS.len()],
            epic: (n % 3 != 2).then_some(EPICS[n % EPICS.len()]),
            agent: AgentKind::ALL[n % AgentKind::ALL.len()],
            age: format!("{}d", n % 30 + 1).into(),
            date: format!("{} {}", MONTHS[n % MONTHS.len()], n % 28 + 1).into(),
        })
        .collect()
}

#[derive(Default)]
struct FrameLog {
    samples: Vec<f64>,
}

impl FrameLog {
    fn push(&mut self, took: Duration) {
        self.samples.push(took.as_secs_f64() * 1000.0);
        if self.samples.len() < FRAME_SAMPLES {
            return;
        }
        self.samples.sort_by(f64::total_cmp);
        let at = |share: usize| {
            self.samples
                .get(self.samples.len() * share / 100)
                .copied()
                .unwrap_or_default()
        };
        let worst = self.samples.last().copied().unwrap_or_default();
        eprintln!(
            "book: board 150 tickets, {} frames render to paint, median {:.2} ms, p95 {:.2} ms, worst {:.2} ms",
            self.samples.len(),
            at(50),
            at(95),
            worst
        );
        self.samples.clear();
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Prop {
    Status,
    Priority,
    Assignee,
}

impl Prop {
    fn choices(self) -> Vec<SharedString> {
        match self {
            Prop::Status => TicketStatus::ALL.map(|s| s.label().into()).to_vec(),
            Prop::Priority => Priority::ALL.map(|p| p.label().into()).to_vec(),
            Prop::Assignee => ASSIGNEES
                .map(|(kind, n)| format!("{} {n}", kind.name()).into())
                .to_vec(),
        }
    }
}

struct Picks {
    status: TicketStatus,
    priority: Priority,
    assignee: (AgentKind, u32),
    open: Prop,
    picker: Presence,
}

impl Picks {
    fn pick(&mut self, at: usize) {
        match self.open {
            Prop::Status => self.status = TicketStatus::ALL.get(at).copied().unwrap_or(self.status),
            Prop::Priority => {
                self.priority = Priority::ALL.get(at).copied().unwrap_or(self.priority)
            }
            Prop::Assignee => self.assignee = ASSIGNEES.get(at).copied().unwrap_or(self.assignee),
        }
    }
}

pub(super) struct BoardyPage {
    picks: Picks,
    tickets: Vec<Ticket>,
    tab: usize,
    folded: Vec<TicketStatus>,
    frames: Entity<FrameLog>,
    tracker: Entity<FlipTracker>,
}

impl BoardyPage {
    pub(super) fn new(cx: &mut Context<Book>) -> Self {
        BoardyPage {
            picks: Picks {
                status: TicketStatus::Review,
                priority: Priority::P1,
                assignee: (AgentKind::GoDev, 2),
                open: Prop::Status,
                picker: Presence::new(PANEL_IN_MS, PANEL_OUT_MS),
            },
            tickets: fixture(),
            tab: 2,
            folded: vec![TicketStatus::Done],
            frames: cx.new(|_| FrameLog::default()),
            tracker: cx.new(|_| FlipTracker::default()),
        }
    }

    pub(super) fn render(
        &self,
        page: Page,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Book>,
    ) -> Div {
        match page {
            Page::StatusIcon => status_page(theme),
            Page::PriorityIcon => priority_page(theme),
            Page::NavSidebar => nav_page(theme, cx),
            Page::PillTabs => self.tabs_page(theme, cx),
            Page::DotChip => chips_page(theme, window, cx),
            Page::Board => self.board_page(theme, cx),
            Page::IssueRow => self.list_page(theme, cx),
            Page::Breadcrumb => crumbs_page(theme, cx),
            Page::Properties => properties_page(&self.picks, theme, window, cx),
            _ => div(),
        }
    }

    fn chips(&self, ticket: &Ticket, theme: &Theme) -> Vec<AnyElement> {
        let (name, tone) = ticket.label;
        let mut chips = vec![
            priority_icon(ticket.priority).into_any_element(),
            dot_chip(name, rgb_to_hsla(theme.color(tone))).into_any_element(),
        ];
        chips.extend(
            ticket
                .epic
                .map(|epic| icon_chip(Glyph::Epic, epic).into_any_element()),
        );
        chips
    }

    fn board_page(&self, theme: &Theme, cx: &mut Context<Book>) -> Div {
        let started = Instant::now();
        let columns: Vec<_> = TicketStatus::ALL
            .into_iter()
            .map(|status| {
                let cards: Vec<_> = self
                    .tickets
                    .iter()
                    .enumerate()
                    .filter(|(_, ticket)| ticket.status == status)
                    .map(|(at, ticket)| {
                        let card = board_card(("card", at))
                            .id_line(ticket.key.clone())
                            .status(ticket.status)
                            .title(ticket.title.clone())
                            .chips(self.chips(ticket, theme))
                            .footer(format!("Created {}", ticket.date))
                            .avatar(agent_avatar(("card-avatar", at), ticket, theme))
                            .selected(at == 0)
                            .on_click(cx.listener(move |book, _: &ClickEvent, _, cx| {
                                if let Some(ticket) = book.boardy.tickets.get_mut(at) {
                                    ticket.status = next_status(ticket.status);
                                }
                                cx.notify();
                            }));
                        flip(("glide", at), &self.tracker, div().child(card))
                    })
                    .collect();
                let header = ColumnHeader {
                    status,
                    title: status.label().into(),
                    count: cards.len(),
                };
                board_column(("column", status as usize), header, cards)
                    .on_more(Book::clicked("Column menu", cx))
                    .on_add(Book::clicked("Add to column", cx))
            })
            .collect();
        let frames = self.frames.clone();
        div()
            .flex()
            .flex_col()
            .gap_2()
            .child(label(
                "150 tickets in nine columns. Click a card to move it to the next status; it glides there. The wheel scrolls a column; Shift and the wheel, a trackpad or the bar at the foot scroll the board sideways. Every 60 frames the book prints render-to-paint times for this page to stderr.",
                theme,
            ))
            .child(
                div()
                    .relative()
                    .h(px(BOARD_HEIGHT))
                    .flex()
                    .child(board("boardy-board", columns))
                    .child(
                        canvas(
                            |_, _, _| (),
                            move |_, (), _, cx| {
                                frames.update(cx, |log, _| log.push(started.elapsed()))
                            },
                        )
                        .absolute()
                        .size_full(),
                    ),
            )
    }

    fn list_page(&self, theme: &Theme, cx: &mut Context<Book>) -> Div {
        let mut list = div().flex().flex_col().gap_0p5().p_1();
        for status in TicketStatus::ALL {
            let rows: Vec<_> = self
                .tickets
                .iter()
                .enumerate()
                .filter(|(_, ticket)| ticket.status == status)
                .collect();
            let shut = self.folded.contains(&status);
            list = list.child(
                group_header(
                    status,
                    status.label(),
                    rows.len(),
                    shut,
                    Book::clicked("Add to group", cx),
                )
                .on_toggle(cx.listener(move |book, _: &ClickEvent, _, cx| {
                    let folded = &mut book.boardy.folded;
                    match folded.iter().position(|seen| *seen == status) {
                        Some(at) => {
                            folded.remove(at);
                        }
                        None => folded.push(status),
                    }
                    cx.notify();
                })),
            );
            if shut {
                continue;
            }
            list = list.children(rows.into_iter().map(|(at, ticket)| {
                let (name, tone) = ticket.label;
                issue_row(("row", at))
                    .priority(ticket.priority)
                    .ticket(ticket.key.clone())
                    .status(ticket.status)
                    .age(ticket.age.clone())
                    .title(ticket.title.clone())
                    .chips([dot_chip(name, rgb_to_hsla(theme.color(tone)))])
                    .avatar(agent_avatar(("row-avatar", at), ticket, theme))
                    .date(ticket.date.clone())
                    .selected(at == 0)
                    .on_click(Book::clicked("Row", cx))
            }));
        }
        div()
            .flex()
            .flex_col()
            .gap_2()
            .child(label(
                "The same 150 tickets as a list grouped by status. Click a group header to fold it; Done starts folded. The first row is selected.",
                theme,
            ))
            .child(
                div()
                    .h(px(LIST_HEIGHT))
                    .flex()
                    .flex_col()
                    .child(ScrollArea::new("boardy-list").child(list)),
            )
    }

    fn tabs_page(&self, theme: &Theme, cx: &mut Context<Book>) -> Div {
        let names = ["Active", "Backlog", "All issues"];
        let mut tabs = PillTabs::new("boardy-tabs");
        for (at, name) in names.into_iter().enumerate() {
            let pick = cx.listener(move |book, _: &ClickEvent, _, cx| {
                book.boardy.tab = at;
                cx.notify();
            });
            tabs = tabs.tab(name, at == self.tab, pick);
        }
        let shown = names.get(self.tab).copied().unwrap_or_default();
        block(
            "Pill tabs: click one; the rest show hover and press",
            theme,
            div()
                .flex()
                .flex_col()
                .gap_3()
                .child(tabs)
                .child(label(format!("Showing {shown}"), theme)),
        )
    }
}

fn next_status(status: TicketStatus) -> TicketStatus {
    let at = TicketStatus::ALL
        .iter()
        .position(|seen| *seen == status)
        .map_or(0, |at| (at + 1) % TicketStatus::ALL.len());
    TicketStatus::ALL.get(at).copied().unwrap_or(status)
}

fn agent_avatar(id: (&'static str, usize), ticket: &Ticket, theme: &Theme) -> Div {
    let agent = Agent {
        kind: ticket.agent,
        instance: 1,
        status: AgentStatus::Finished,
    };
    avatar(id, &agent, AvatarSize::Feed, theme)
}

fn status_page(theme: &Theme) -> Div {
    let row = TicketStatus::ALL.map(|status| {
        named(
            status.label(),
            theme,
            div().child(status_icon(status, px(ICON))),
        )
    });
    let sizes = TicketStatus::ALL.map(|status| {
        named(
            status.label(),
            theme,
            div()
                .flex()
                .items_center()
                .gap_2()
                .children(STATUS_SIZES.map(|size| status_icon(status, px(size)))),
        )
    });
    div()
        .flex()
        .flex_col()
        .gap_3()
        .child(block(
            "All nine statuses side by side at 16 px",
            theme,
            spread(theme).gap_6().children(row),
        ))
        .child(block(
            "Each at 12, 14, 16 and 24 px",
            theme,
            spread(theme).gap_6().children(sizes),
        ))
}

fn priority_page(theme: &Theme) -> Div {
    let all = Priority::ALL.map(|priority| {
        named(
            format!("{priority:?} {}", priority.label()),
            theme,
            div().child(priority_icon(priority)),
        )
    });
    block(
        "P0 to P4: urgent, three bars to one, none",
        theme,
        spread(theme).gap_6().children(all),
    )
}

fn nav(id: &'static str, words: [&'static str; 4], cx: &mut Context<Book>) -> NavSidebar {
    let [tickets, epics, sprints, board] = words;
    let recent = [
        (TicketStatus::Doing, "DESK-386 Boardy screen", true),
        (TicketStatus::Doing, "DESK-385 Card move motion", true),
        (TicketStatus::Review, "DESK-384 Boardy UI kit", false),
        (TicketStatus::NeedsDecision, "TOFU-912 boardy.plan", false),
        (TicketStatus::Todo, "DESK-383 Settings choice rows", false),
    ];
    let mut newest = NavSidebar::new((id, 4usize));
    for (status, name, running) in recent {
        newest = newest.item(status, name, None, running, false, Book::clicked(name, cx));
    }
    NavSidebar::new(id)
        .item(
            Glyph::Catchup,
            "Catch up",
            None,
            false,
            false,
            Book::clicked("Catch up", cx),
        )
        .item(
            Glyph::Attention,
            "Need attention",
            Some(7),
            true,
            false,
            Book::clicked("Need attention", cx),
        )
        .group(
            format!("{board}s"),
            false,
            NavSidebar::new((id, 1usize))
                .group(
                    "DESK",
                    false,
                    NavSidebar::new((id, 2usize))
                        .item(
                            Glyph::Inbox,
                            tickets,
                            Some(150),
                            false,
                            true,
                            Book::clicked(tickets, cx),
                        )
                        .item(
                            Glyph::Epic,
                            epics,
                            Some(3),
                            false,
                            false,
                            Book::clicked(epics, cx),
                        )
                        .item(
                            Glyph::Milestone,
                            "Milestones",
                            Some(2),
                            false,
                            false,
                            Book::clicked("Milestones", cx),
                        )
                        .item(
                            Glyph::Sprint,
                            sprints,
                            None,
                            false,
                            false,
                            Book::clicked(sprints, cx),
                        )
                        .item(
                            TicketStatus::Triage,
                            "Triage",
                            Some(6),
                            false,
                            false,
                            Book::clicked("Triage", cx),
                        ),
                )
                .group(
                    "TOFU",
                    true,
                    NavSidebar::new((id, 3usize)).item(
                        Glyph::Inbox,
                        tickets,
                        Some(912),
                        false,
                        false,
                        Book::clicked(tickets, cx),
                    ),
                ),
        )
        .group("Recent", false, newest)
}

fn nav_page(theme: &Theme, cx: &mut Context<Book>) -> Div {
    let frame = |sidebar: NavSidebar| {
        div()
            .h(px(NAV_HEIGHT))
            .rounded_md()
            .bg(theme.color(ColorToken::SurfaceWindow))
            .p_1()
            .child(sidebar)
    };
    let jira = nav("nav-jira", ["Tickets", "Epics", "Sprints", "Board"], cx);
    let linear = nav("nav-linear", ["Issues", "Projects", "Cycles", "Team"], cx);
    let glyphs = GLYPHS.map(|(shown, name)| {
        let color = theme.color(ColorToken::TextIcon);
        named(
            name,
            theme,
            div()
                .flex()
                .items_center()
                .gap_2()
                .child(glyph(shown, ICON_TINY, color))
                .child(glyph(shown, ICON_SMALL, color))
                .child(glyph(shown, ICON, color)),
        )
    });
    div()
        .flex()
        .flex_col()
        .gap_3()
        .child(block(
            "Catch up, need attention, boards with their sub items, the five newest. Hover glides; group titles fold (TOFU starts folded).",
            theme,
            spread(theme)
                .items_start()
                .gap_6()
                .child(named("boardFlow jira", theme, frame(jira)))
                .child(named("boardFlow linear", theme, frame(linear))),
        ))
        .child(block(
            "Boardy glyphs at 11, 13 and 16",
            theme,
            spread(theme).gap_6().children(glyphs),
        ))
}

fn chips_page(theme: &Theme, window: &mut Window, cx: &mut Context<Book>) -> Div {
    let dots = LABELS.map(|(name, tone)| dot_chip(name, rgb_to_hsla(theme.color(tone))));
    block(
        "Dot chips, icon chips and a label too long for its chip",
        theme,
        div()
            .flex()
            .flex_col()
            .gap_3()
            .child(spread(theme).children(dots))
            .child(
                spread(theme)
                    .child(icon_chip(Glyph::Epic, "Boardy in the desk"))
                    .child(icon_chip(Glyph::Sprint, "Cycle 12"))
                    .child(icon_chip(Glyph::Milestone, "0.6.0"))
                    .child(icon_chip(Glyph::Label, "needs-design")),
            )
            .child(spread(theme).child(tooltip(
                "long-chip-tip",
                div().id("long-chip").child(dot_chip(
                    LONG_LABEL,
                    rgb_to_hsla(theme.color(ColorToken::StatusWarn)),
                )),
                Edge::Frame,
                LONG_LABEL,
                theme,
                window,
                cx,
            ))),
    )
}

fn crumbs_page(theme: &Theme, cx: &mut Context<Book>) -> Div {
    let link = |what: &'static str, cx: &mut Context<Book>| -> Option<OnCrumb> {
        Some(Box::new(Book::clicked(what, cx)))
    };
    let ticket = breadcrumb([
        ("DESK", link("DESK", cx)),
        ("Tickets", link("Tickets", cx)),
        (
            "DESK-384 Boardy UI kit: icons, sidebar, tabs, chips, board",
            None,
        ),
    ]);
    let short = breadcrumb([("TOFU", link("TOFU", cx)), ("Catch up", None)]);
    block(
        "Every crumb but the last is a link; the last truncates",
        theme,
        div()
            .flex()
            .flex_col()
            .gap_3()
            .child(div().id("crumbs-ticket").max_w(px(520.0)).child(ticket))
            .child(div().id("crumbs-short").child(short)),
    )
}

fn properties_page(
    picks: &Picks,
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<Book>,
) -> Div {
    let line = |mark: AnyElement, text: SharedString| {
        div()
            .flex()
            .items_center()
            .gap_1p5()
            .child(mark)
            .child(text)
            .into_any_element()
    };
    let reduced = reduced_motion(cx);
    let mut picker = |prop: Prop, id: &'static str, shown: AnyElement| {
        let items = actions(prop.choices());
        let list = div().absolute().top(px(PICKER_DROP)).left_0().child(menu(
            "property-picker",
            &items,
            theme,
            cx.listener(|book, at: &usize, _, cx| {
                let picks = &mut book.boardy.picks;
                picks.pick(*at);
                picks.picker.set_open(false, Instant::now());
                cx.notify();
            }),
        ));
        let opened = (picks.open == prop)
            .then(|| present(&picks.picker, reduced, window, list))
            .flatten();
        div()
            .relative()
            .child(
                div()
                    .id(id)
                    .cursor_pointer()
                    .child(shown)
                    .on_click(cx.listener(move |book, _: &ClickEvent, _, cx| {
                        let picks = &mut book.boardy.picks;
                        if picks.open == prop {
                            toggle(&mut picks.picker);
                        } else {
                            picks.open = prop;
                            picks.picker.set_open(true, Instant::now());
                        }
                        cx.notify();
                    })),
            )
            .children(opened.map(|menu| deferred(menu).priority(1)))
            .into_any_element()
    };
    let (kind, instance) = picks.assignee;
    let agent = Agent {
        kind,
        instance,
        status: AgentStatus::Finished,
    };
    let status = picker(
        Prop::Status,
        "prop-status",
        line(
            status_icon(picks.status, px(14.0)).into_any_element(),
            picks.status.label().into(),
        ),
    );
    let priority = picker(
        Prop::Priority,
        "prop-priority",
        line(
            priority_icon(picks.priority).into_any_element(),
            picks.priority.label().into(),
        ),
    );
    let assignee = picker(
        Prop::Assignee,
        "prop-assignee",
        line(
            avatar("prop-avatar", &agent, AvatarSize::Feed, theme).into_any_element(),
            format!("{} {instance}", kind.name()).into(),
        ),
    );
    let panel = properties_panel()
        .section(
            "Properties",
            [
                ("Status".into(), status),
                ("Priority".into(), priority),
                ("Assignee".into(), assignee),
                (
                    "Epic".into(),
                    icon_chip(Glyph::Epic, "Boardy in the desk").into_any_element(),
                ),
                (
                    "Sprint".into(),
                    icon_chip(Glyph::Sprint, "Cycle 12").into_any_element(),
                ),
            ],
        )
        .section(
            "Labels",
            [(
                SharedString::default(),
                div()
                    .flex()
                    .flex_wrap()
                    .gap_1()
                    .child(dot_chip(
                        "Feature",
                        rgb_to_hsla(theme.color(ColorToken::AnsiMagenta)),
                    ))
                    .child(dot_chip(
                        "Docs",
                        rgb_to_hsla(theme.color(ColorToken::AnsiBlue)),
                    ))
                    .into_any_element(),
            )],
        )
        .section(
            "Owns",
            [(
                SharedString::default(),
                code("apps/desk/crates/ui/src/components/board.rs", theme).into_any_element(),
            )],
        )
        .section(
            "Related",
            [(
                "Blocks".into(),
                line(
                    status_icon(TicketStatus::Doing, px(14.0)).into_any_element(),
                    "DESK-386 Boardy screen".into(),
                ),
            )],
        );
    block("The ticket page's right column", theme, div().child(panel))
}
