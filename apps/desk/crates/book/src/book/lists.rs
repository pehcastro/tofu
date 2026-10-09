use std::cell::RefCell;
use std::rc::Rc;

use desk_ui::components::avatar::Agent;
use desk_ui::components::avatar::{AgentStatus, AvatarSize, avatar};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{Header, inner_card, shell};
use desk_ui::components::chip::{GitStatus, git_name, trace};
use desk_ui::components::feed::{Feed, FeedEntry, FeedEvent};
use desk_ui::components::find::{FindGroup, find_results};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::list::{
    HoverVariant, RowGlide, bare_row, group_header, row, separator, table,
};
use desk_ui::components::paint::{glyph, ink};
use desk_ui::components::scroll::{ScrollArea, list_scrollbar};
use desk_ui::components::size::{CAPTION_TEXT, FONT_CHAT, T1};
use desk_ui::components::tabs::{Tab, TabEvent, TabFlag, TabMark, TabStrip};
use desk_ui::icon::Icon;
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::ICON_SMALL;
use desk_ui::theme::Theme;
use gpui::{
    App, ClickEvent, Context, Div, FontWeight, SharedString, Stateful, UniformListScrollHandle,
    Window, div, prelude::*, px,
};

use super::Book;
use super::kit::{LIST_HEIGHT, TILE_HEIGHT, block, label, titled};
use crate::catalog::Page;
use crate::data;

const FEED_TOKENS: [u32; 5] = [12_400, 830, 3_150, 48_900, 7_020];
const SELECTED_ROW: usize = 2;
pub const CONNECTED_ROOM: usize = 4;
const STATIC_ROW_WIDTH: f32 = 360.0;
const SHORT_FIT_WIDTH: f32 = 360.0;
const SHORT_FOLD_WIDTH: f32 = 200.0;
const CROWDED_WIDTH: f32 = 300.0;
const EXPANDED_TIP: &str =
    "Expanded view of Sub-agents in editor. Closing this tab will not close the tile there.";

#[derive(Clone)]
pub struct Strip {
    tabs: Vec<Tab>,
    screens: usize,
    picked: usize,
    made: usize,
}

impl Strip {
    pub fn modules() -> Self {
        let tabs = [
            ("Sub-agents", Some(36), Glyph::Agents),
            ("File edits", Some(50), Glyph::File),
            ("Shells", Some(6), Glyph::Terminal),
            ("Editor", None, Glyph::File),
            ("Terminal", None, Glyph::Terminal),
            ("Browser", None, Glyph::Chat),
        ]
        .map(|(name, count, icon)| Tab {
            label: name.into(),
            icon: Some(icon),
            count,
            mark: TabMark::Close,
            flag: None,
        });
        Strip {
            tabs: tabs.to_vec(),
            screens: 0,
            picked: 0,
            made: 0,
        }
    }

    fn files() -> Self {
        let tabs =
            [("form.rs", TabMark::Dirty), ("tabs.rs", TabMark::Close)].map(|(name, mark)| Tab {
                label: name.into(),
                icon: Some(Glyph::File),
                count: None,
                mark,
                flag: None,
            });
        Strip {
            tabs: tabs.to_vec(),
            screens: 0,
            picked: 0,
            made: 0,
        }
    }

    pub(super) fn pair() -> Self {
        let mut strip = Strip::modules();
        strip.tabs.truncate(2);
        strip
    }

    fn workspaces(screen: bool) -> Self {
        let mut tabs: Vec<Tab> = [
            ("work", TabMark::Pinned),
            ("editor", TabMark::Close),
            ("data", TabMark::Locked),
            ("review DESK-003", TabMark::Close),
        ]
        .map(|(name, mark)| Tab {
            label: name.into(),
            icon: None,
            count: None,
            mark,
            flag: None,
        })
        .to_vec();
        let screens = if screen {
            tabs.push(Tab {
                label: "Sub-agents".into(),
                icon: None,
                count: None,
                mark: TabMark::Close,
                flag: Some(TabFlag {
                    icon: Icon::Expand,
                    tip: EXPANDED_TIP.into(),
                }),
            });
            tabs.push(Tab {
                label: "Settings".into(),
                icon: None,
                count: None,
                mark: TabMark::Close,
                flag: None,
            });
            2
        } else {
            0
        };
        Strip {
            tabs,
            screens,
            picked: 1,
            made: 0,
        }
    }

    fn crowded() -> Self {
        let mut strip = Strip::workspaces(true);
        strip.tabs.extend(["Usage", "Library"].map(|name| Tab {
            label: name.into(),
            icon: None,
            count: None,
            mark: TabMark::Close,
            flag: None,
        }));
        strip.screens += 2;
        strip
    }

    fn short() -> Self {
        let tabs = ["work", "logs", "diff"].map(|name| Tab {
            label: name.into(),
            icon: None,
            count: None,
            mark: TabMark::Close,
            flag: None,
        });
        Strip {
            tabs: tabs.to_vec(),
            screens: 0,
            picked: 0,
            made: 0,
        }
    }

    fn split(&self) -> (&[Tab], &[Tab]) {
        self.tabs
            .split_at(self.tabs.len().saturating_sub(self.screens))
    }

    pub fn connected(
        &self,
        id: &'static str,
        room: usize,
        theme: &Theme,
        cx: &mut Context<Book>,
        strip: impl Fn(&mut Book) -> Option<&mut Strip> + 'static,
    ) -> TabStrip {
        TabStrip::connected(
            id,
            &self.tabs,
            self.picked,
            room,
            theme,
            cx.listener(move |this, event: &TabEvent, _, cx| {
                if let Some(strip) = strip(this) {
                    strip.apply(*event);
                    cx.notify();
                }
            }),
        )
    }

    fn header(
        &self,
        id: &'static str,
        theme: &Theme,
        cx: &mut Context<Book>,
        strip: impl Fn(&mut Book) -> Option<&mut Strip> + 'static,
    ) -> TabStrip {
        let (tabs, screens) = self.split();
        TabStrip::header(
            id,
            tabs,
            self.picked,
            screens,
            theme,
            cx.listener(move |this, event: &TabEvent, _, cx| {
                if let Some(strip) = strip(this) {
                    strip.apply(*event);
                    cx.notify();
                }
            }),
        )
    }

    fn apply(&mut self, event: TabEvent) {
        let workspaces = self.tabs.len().saturating_sub(self.screens);
        match event {
            TabEvent::Select(at) => self.picked = at,
            TabEvent::Close(at) if at < self.tabs.len() => {
                self.tabs.remove(at);
                if at >= workspaces {
                    self.screens = self.screens.saturating_sub(1);
                }
                if at < self.picked {
                    self.picked -= 1;
                }
                self.picked = self.picked.min(self.tabs.len().saturating_sub(1));
            }
            TabEvent::Close(_) => {}
            TabEvent::New => {
                self.made += 1;
                self.tabs.insert(
                    workspaces,
                    Tab {
                        label: format!("new {}", self.made).into(),
                        icon: None,
                        count: None,
                        mark: TabMark::Close,
                        flag: None,
                    },
                );
                self.picked = workspaces;
            }
        }
    }

    pub fn content(&self) -> Div {
        let Some(tab) = self.tabs.get(self.picked) else {
            return div()
                .font_weight(FontWeight::SEMIBOLD)
                .child("Every tab is closed. Reset brings them back.");
        };
        let lines: Vec<SharedString> = match tab.label.as_ref() {
            "Sub-agents" => vec![
                "scout    reading internal/turn/loop.go".into(),
                "builder  writing crates/book/src/book/lists.rs".into(),
                "judge    waiting on review of DESK-053".into(),
            ],
            "File edits" => vec![
                "M  crates/book/src/book/lists.rs   +42 -9".into(),
                "M  crates/ui/src/components/tabs.rs   +7 -2".into(),
                "A  crates/motion/src/glide.rs   +118".into(),
                "D  book/themes/book-light.json   -64".into(),
            ],
            "Shells" => vec![
                "$ cargo check -p desk_book -j 2".into(),
                "Finished `dev` profile in 1.6s, exit 0".into(),
            ],
            "Editor" => vec![
                "1  fn apply(&mut self, event: TabEvent) {".into(),
                "2      match event {".into(),
                "3          TabEvent::Select(at) => self.picked = at,".into(),
            ],
            "Terminal" => vec!["PS F:\\localhost\\ephem-sh\\tofu> git status --short".into()],
            "Browser" => vec![
                "localhost:5173/book/tabs".into(),
                "200 OK, 14 requests, 312 kB".into(),
            ],
            "work" => vec!["Pinned workspace: the lead and 6 agents".into()],
            "editor" => vec![
                "Two files open side by side".into(),
                "lists.rs and tabs.rs".into(),
            ],
            "data" => vec!["Locked workspace: ledger.db, 5,000 rows, read only".into()],
            "review DESK-003" => vec![
                "3 comments, 1 unresolved".into(),
                "Approve, or send back to rust-dev".into(),
            ],
            "Settings" => vec!["Screen tab: theme, mode, keys, accounts".into()],
            "form.rs" => vec!["Edited and not saved: the dot stands where the \u{d7} was".into()],
            "tabs.rs" => vec!["Saved: its \u{d7} closes it".into()],
            other => vec![format!("{other}: an empty workspace, nothing open yet").into()],
        };
        div()
            .flex()
            .flex_col()
            .gap_1()
            .child(
                div()
                    .font_weight(FontWeight::SEMIBOLD)
                    .child(tab.label.clone()),
            )
            .children(lines.into_iter().map(|line| div().child(line)))
    }
}

pub struct TabPage {
    module: Strip,
    roomy: Strip,
    header: Strip,
    bare: Strip,
    files: Strip,
    fit: Strip,
    fold: Strip,
    crowded: Strip,
}

impl TabPage {
    pub fn new() -> Self {
        TabPage {
            module: Strip::modules(),
            roomy: Strip::modules(),
            header: Strip::workspaces(true),
            bare: Strip::workspaces(false),
            files: Strip::files(),
            fit: Strip::short(),
            fold: Strip::short(),
            crowded: Strip::crowded(),
        }
    }
}

pub(super) struct ListsState {
    tabs: TabPage,
    agents: Vec<Agent>,
    files: Rc<Vec<(GitStatus, SharedString)>>,
    feed: Feed,
    feed_clicked: Rc<RefCell<SharedString>>,
    table: UniformListScrollHandle,
    open_groups: [bool; 4],
    file_row: usize,
}

#[derive(Clone, Copy)]
enum Entry {
    Group(AgentStatus, usize, bool),
    Agent(Agent),
}

type GlidedRow = Rc<dyn Fn(usize, &Theme) -> Stateful<Div>>;

#[derive(IntoElement)]
struct GlidedList {
    id: &'static str,
    count: usize,
    item: GlidedRow,
}

impl RenderOnce for GlidedList {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let GlidedList { id, count, item } = self;
        let theme = ActiveTheme::theme(cx);
        let glide = RowGlide::new(
            SharedString::from(format!("{id}-glide")),
            HoverVariant::Glide,
            &theme,
            window,
            cx,
        );
        let list = ScrollArea::new(SharedString::from(format!("{id}-scroll")))
            .children((0..count).map(|ix| glide.row(ix, item(ix, &theme))));
        glide
            .frame(SharedString::from(format!("{id}-frame")), list)
            .flex()
            .flex_col()
            .size_full()
            .min_h_0()
    }
}

fn section(heading: &'static str, about: &'static str, theme: &Theme) -> Div {
    div().flex().flex_col().gap_3().child(
        div()
            .flex()
            .flex_col()
            .gap_1()
            .child(
                div()
                    .text_size(px(FONT_CHAT))
                    .font_weight(FontWeight::SEMIBOLD)
                    .text_color(ink(theme, T1))
                    .child(heading),
            )
            .child(label(about, theme)),
    )
}

fn sections() -> Div {
    div().flex().flex_col().gap_6()
}

impl ListsState {
    pub(super) fn new() -> Self {
        let agents = data::agents();
        let entries = data::feed(&agents)
            .into_iter()
            .zip(FEED_TOKENS.iter().cycle())
            .map(|((agent, event, time), &tokens)| FeedEntry {
                agent,
                diff: match event {
                    FeedEvent::Edit { added, removed, .. } => Some((added, removed)),
                    _ => None,
                },
                event,
                tokens,
                time,
            })
            .collect();
        let feed_clicked = Rc::new(RefCell::new(SharedString::default()));
        let (expanded, mentioned) = (feed_clicked.clone(), feed_clicked.clone());
        ListsState {
            tabs: TabPage::new(),
            feed: Feed::new(entries)
                .on_expand(move |agent, window, _| {
                    *expanded.borrow_mut() = format!("expand {}", agent.name()).into();
                    window.refresh();
                })
                .on_mention(move |agent, window, _| {
                    *mentioned.borrow_mut() = format!("mention {}", agent.name()).into();
                    window.refresh();
                }),
            feed_clicked,
            agents,
            files: Rc::new(data::files()),
            table: UniformListScrollHandle::new(),
            open_groups: [true, true, true, false],
            file_row: 0,
        }
    }

    pub(super) fn render(&mut self, page: Page, theme: &Theme, cx: &mut Context<Book>) -> Div {
        match page {
            Page::Tabs => self.tabs(theme, cx),
            Page::Row => self.rows(theme, cx),
            Page::Table => self.table(theme),
            Page::Feed => self.feed(theme),
            _ => div(),
        }
    }

    fn tabs(&self, theme: &Theme, cx: &mut Context<Book>) -> Div {
        let page = &self.tabs;
        let module = page
            .module
            .connected("module-tabs", CONNECTED_ROOM, theme, cx, |book| {
                Some(&mut book.lists.tabs.module)
            });
        let roomy = page
            .roomy
            .connected("roomy-tabs", usize::MAX, theme, cx, |book| {
                Some(&mut book.lists.tabs.roomy)
            });
        let header = page.header.header("header-tabs", theme, cx, |book| {
            Some(&mut book.lists.tabs.header)
        });
        let bare = page.bare.header("header-bare", theme, cx, |book| {
            Some(&mut book.lists.tabs.bare)
        });
        let files = page
            .files
            .connected("file-tabs", usize::MAX, theme, cx, |book| {
                Some(&mut book.lists.tabs.files)
            });
        let fit = page.fit.header("short-fit", theme, cx, |book| {
            Some(&mut book.lists.tabs.fit)
        });
        let fold = page.fold.header("short-fold", theme, cx, |book| {
            Some(&mut book.lists.tabs.fold)
        });
        let crowded = page.crowded.header("crowded-tabs", theme, cx, |book| {
            Some(&mut book.lists.tabs.crowded)
        });
        let tile = |strip: TabStrip, shown: &Strip, about: &'static str| {
            shell(Header::Tabs(strip.into_any_element(), None), theme)
                .min_h(px(TILE_HEIGHT / 2.0))
                .child(
                    inner_card(theme)
                        .p_3()
                        .gap_3()
                        .child(shown.content())
                        .child(label(about, theme)),
                )
        };
        let workspace = |strip: TabStrip, shown: &Strip| {
            div()
                .flex()
                .flex_col()
                .gap_3()
                .child(strip)
                .child(shown.content())
        };
        let reset = button("tabs-reset", "Reset tabs", None, ButtonKind::Plain, theme).on_click(
            cx.listener(|this, _: &ClickEvent, _, cx| {
                this.lists.tabs = TabPage::new();
                cx.notify();
            }),
        );
        sections()
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_3()
                    .child(reset)
                    .child(label(
                        "Brings back every closed tab and drops the added ones.",
                        theme,
                    )),
            )
            .child(
                section(
                    "Tile tabs",
                    "Connected tabs in a tile header. Click one to switch the panel under it, its \u{d7} closes it.",
                    theme,
                )
                .child(tile(
                    module,
                    &page.module,
                    "Room for 4: N more lists the hidden tabs.",
                ))
                .child(tile(roomy, &page.roomy, "Room for all six.")),
            )
            .child(
                section(
                    "Workspace tabs",
                    "Title bar tabs: pinned, closable, locked, and + adds one.",
                    theme,
                )
                .child(block(
                    "With a screen tab",
                    theme,
                    workspace(header, &page.header),
                ))
                .child(block(
                    "Without a screen tab",
                    theme,
                    workspace(bare, &page.bare),
                ))
                .child(block(
                    "Three short tabs with room: all shown",
                    theme,
                    div().w(px(SHORT_FIT_WIDTH)).child(workspace(fit, &page.fit)),
                ))
                .child(block(
                    "The same three without room: they fold",
                    theme,
                    div().w(px(SHORT_FOLD_WIDTH)).child(workspace(fold, &page.fold)),
                ))
                .child(block(
                    "Three screens without room: the strip eases to the chosen tab, N more stays",
                    theme,
                    div()
                        .w(px(CROWDED_WIDTH))
                        .child(workspace(crowded, &page.crowded)),
                )),
            )
            .child(
                section(
                    "File tabs",
                    "An editor's open files. A dot marks one with unsaved changes.",
                    theme,
                )
                .child(tile(files, &page.files, "form.rs is dirty, tabs.rs is saved.")),
            )
    }

    fn rows(&self, theme: &Theme, cx: &mut Context<Book>) -> Div {
        let mut entries = Vec::new();
        for (at, status) in AgentStatus::ALL.into_iter().enumerate() {
            let members = self.agents.iter().filter(|agent| agent.status == status);
            let open = self.open_groups.get(at).copied().unwrap_or(false);
            entries.push(Entry::Group(status, members.clone().count(), open));
            if open {
                entries.extend(members.copied().map(Entry::Agent));
            }
        }
        let book = cx.entity();
        let agents = GlidedList {
            id: "agents",
            count: entries.len(),
            item: Rc::new(move |ix, theme| match entries.get(ix).copied() {
                Some(Entry::Group(status, members, open)) => {
                    let book = book.clone();
                    group_header(("group", ix), status.group(), members, open, theme).on_click(
                        move |_, _, cx| {
                            book.update(cx, |this, cx| {
                                let at = AgentStatus::ALL.iter().position(|known| *known == status);
                                if let Some(open) =
                                    at.and_then(|at| this.lists.open_groups.get_mut(at))
                                {
                                    *open = !*open;
                                    cx.notify();
                                }
                            })
                        },
                    )
                }
                Some(Entry::Agent(agent)) => bare_row(("agent", ix), false, false, theme)
                    .child(avatar(
                        ("agent-avatar", ix),
                        &agent,
                        AvatarSize::List,
                        theme,
                    ))
                    .child(div().flex_1().child(agent.name()))
                    .child(trace(("agent-trace", ix), agent.name(), theme)),
                None => bare_row(("agent", ix), false, false, theme),
            }),
        };
        let files = self.files.clone();
        let picked_file = self.file_row;
        let book = cx.entity();
        let file_rows = GlidedList {
            id: "files",
            count: files.len(),
            item: Rc::new(move |ix, theme| {
                let picked = ix == picked_file;
                let line = bare_row(("file", ix), picked, picked, theme);
                let Some((status, path)) = files.get(ix) else {
                    return line;
                };
                let book = book.clone();
                line.child(glyph(Glyph::File, ICON_SMALL, ink(theme, CAPTION_TEXT)))
                    .child(git_name(path.clone(), Some(*status), theme).flex_1())
                    .on_click(move |_, _, cx| {
                        book.update(cx, |this, cx| {
                            this.lists.file_row = ix;
                            cx.notify();
                        })
                    })
            }),
        };
        let states = [
            ("rest", false, false),
            ("selected", true, false),
            ("focused", false, true),
            ("selected and focused", true, true),
        ]
        .into_iter()
        .enumerate()
        .map(|(at, (name, selected, focused))| {
            row(("state-row", at), selected, focused, theme).child(div().flex_1().child(name))
        });
        sections()
            .child(
                section(
                    "States",
                    "The four states a row can be in. Hover the rest row.",
                    theme,
                )
                .child(block(
                    "Rest, selected, focused, both",
                    theme,
                    div()
                        .flex()
                        .flex_col()
                        .w(px(STATIC_ROW_WIDTH))
                        .children(states)
                        .child(separator(theme)),
                )),
            )
            .child(
                section(
                    "In a list",
                    "Rows in a scrolling list. The hover bar glides between rows; drag the scrollbar or click its track.",
                    theme,
                )
                .child(
                    div()
                        .flex()
                        .gap_3()
                        .h(px(LIST_HEIGHT))
                        .child(
                            shell(titled("Grouped: 36 agents, click a group to fold it"), theme)
                                .flex_1()
                                .child(inner_card(theme).p_1().child(agents)),
                        )
                        .child(
                            shell(titled("Flat: 50 files, click one to select it"), theme)
                                .flex_1()
                                .child(inner_card(theme).p_1().child(file_rows)),
                        ),
                ),
            )
    }

    fn table(&self, theme: &Theme) -> Div {
        let found = |label: &'static str, hits: &[&'static str]| FindGroup {
            label: label.into(),
            hits: hits.iter().map(|hit| SharedString::from(*hit)).collect(),
        };
        let groups = [
            found(
                "Chat",
                &["list the planets in a table", "the planets route returns"],
            ),
            found("Sub-agents", &["add a planets endpoint to hono"]),
            found("File edits", &["src/routes/planets.ts"]),
        ];
        sections()
            .child(
                section(
                    "Find across tiles",
                    "Ctrl F in a workspace. Hits grouped by tile with counts; row 2 is current.",
                    theme,
                )
                .child(
                    div().w(px(STATIC_ROW_WIDTH)).child(
                        inner_card(theme)
                            .p_1()
                            .child(find_results(&groups, 2, theme, None)),
                    ),
                ),
            )
            .child(
                section(
                    "Table",
                    "5,000 rows, only the visible ones laid out. Hover glides, row 3 is selected.",
                    theme,
                )
                .child(div().h(px(LIST_HEIGHT)).flex().child(
                    shell(titled("Ledger: 5,000 rows"), theme).flex_1().child(
                        inner_card(theme).p_1().child(table(
                            "table",
                            &data::COLUMNS,
                            data::TABLE_ROWS,
                            Some(SELECTED_ROW),
                            &self.table,
                            theme,
                            data::table_cell,
                        )),
                    ),
                )),
            )
    }

    fn feed(&self, theme: &Theme) -> Div {
        let clicked = self.feed_clicked.borrow().clone();
        let clicked = if clicked.is_empty() {
            SharedString::from("Last click: none yet")
        } else {
            SharedString::from(format!("Last click: {clicked}"))
        };
        sections().child(
            section(
                "Feed",
                "One line per agent action, every event kind. Expand or mention an agent from its line.",
                theme,
            )
            .child(
                div().h(px(LIST_HEIGHT)).flex().child(
                    shell(titled("Every event kind"), theme)
                        .flex_1()
                        .child(
                            inner_card(theme).p_1().child(
                                div()
                                    .relative()
                                    .flex()
                                    .flex_col()
                                    .flex_1()
                                    .min_h_0()
                                    .child(self.feed.render(theme))
                                    .child(list_scrollbar("lists-feed-bar", self.feed.state())),
                            ),
                        ),
                ),
            )
            .child(label(clicked, theme)),
        )
    }
}
