use desk_motion::reduced_motion;
use desk_ui::components::chip::GitStatus;
use desk_ui::components::history::{
    Blame, BlameLine, ChangedFile, Changes, Commit, Day, History, blame_gutter, inline_blame,
};
use desk_ui::components::tree::{FileTree, IconTheme, IconThemeError, TreeNode};
use desk_ui::theme::Theme;
use gpui::{Context, Div, Entity, IntoElement, SharedString, Window, div, prelude::*, px};

use super::Book;
use super::kit::{block, label};

const TREE_WIDTH: f32 = 282.0;
const PANEL_PAD_Y: f32 = 8.0;
const PANEL_PAD_X: f32 = 4.0;
const HISTORY_WIDTH: f32 = 460.0;
const BLAME_WIDTH: f32 = 640.0;
const NARROW: f32 = 320.0;
const WRITER: &str = "ts-dev";
const HELD_BRANCH: &str = "feat/sort-order";

fn tree() -> Vec<TreeNode> {
    vec![
        TreeNode::folder(
            ".github",
            None,
            vec![TreeNode::folder(
                "workflows",
                None,
                vec![TreeNode::file("ci.yml", None)],
            )],
        ),
        TreeNode::folder(
            "cmd",
            None,
            vec![TreeNode::folder(
                "notes",
                None,
                vec![TreeNode::file("main.go", None)],
            )],
        ),
        TreeNode::folder(
            "migrations",
            None,
            vec![
                TreeNode::file("0001_notes.sql", None),
                TreeNode::file("0002_tags.sql", Some(GitStatus::Added)),
            ],
        ),
        TreeNode::folder(
            "notes",
            None,
            vec![
                TreeNode::file("notes.go", Some(GitStatus::Modified)),
                TreeNode::file("notes_test.go", Some(GitStatus::Untracked)),
                TreeNode::file("store.go", None),
            ],
        ),
        TreeNode::folder(
            "web",
            None,
            vec![
                TreeNode::file("NoteList.tsx", Some(GitStatus::Modified)),
                TreeNode::file("count.ts", Some(GitStatus::Untracked)),
                TreeNode::file("OldBadge.tsx", Some(GitStatus::Deleted)),
                TreeNode::file("Header.tsx", Some(GitStatus::Conflict)),
            ],
        ),
        TreeNode::folder(
            "node_modules",
            Some(GitStatus::Ignored),
            vec![TreeNode::folder(
                "react",
                Some(GitStatus::Ignored),
                vec![TreeNode::file("package.json", Some(GitStatus::Ignored))],
            )],
        ),
        TreeNode::file(".env", Some(GitStatus::Ignored)),
        TreeNode::file(".gitignore", None),
        TreeNode::file("go.mod", None),
        TreeNode::file("README.md", None),
        TreeNode::file("LICENSE", None),
    ]
}

fn file_tree(id: &'static str, icons: &IconTheme) -> FileTree {
    FileTree::new(id, tree(), icons)
        .closed(".github")
        .closed("cmd")
        .closed("migrations")
        .closed("node_modules")
        .selected("notes/notes.go")
}

fn panel(tree: Entity<FileTree>) -> Div {
    div().py(px(PANEL_PAD_Y)).px(px(PANEL_PAD_X)).child(tree)
}

fn commit(subject: &str, author: &str, age: &str, agent: Option<&str>) -> Commit {
    Commit {
        subject: SharedString::from(subject.to_owned()),
        author: SharedString::from(author.to_owned()),
        age: SharedString::from(age.to_owned()),
        agent: agent.map(|agent| SharedString::from(agent.to_owned())),
    }
}

fn days() -> Vec<Day> {
    vec![
        Day {
            title: "Today".into(),
            commits: vec![
                commit(
                    "fix(undo): snapshot the project root",
                    "pehcastro",
                    "2h",
                    Some("go-dev"),
                ),
                commit("Merge feat/badge-spike", "pehcastro", "3h", None),
                commit(
                    "feat(undo): put back the files a turn changed",
                    "pehcastro",
                    "5h",
                    Some("go-dev"),
                ),
            ],
        },
        Day {
            title: "Yesterday".into(),
            commits: vec![
                commit("docs(changelog): search units", "pehcastro", "1d", None),
                commit(
                    "refactor(turn): one budget per loop",
                    "pehcastro",
                    "1d",
                    Some("go-dev"),
                ),
            ],
        },
    ]
}

fn branches() -> Vec<SharedString> {
    ["develop", HELD_BRANCH, "feat/badge-spike", "main"]
        .map(SharedString::from)
        .to_vec()
}

fn history(held: bool, cx: &mut Context<Book>) -> Entity<History> {
    cx.new(|cx| {
        let mut history = History::new(days(), branches(), Some(WRITER.into()), cx);
        if held {
            history.hold(HELD_BRANCH);
        }
        history
    })
}

fn person(commit: &str) -> Blame {
    Blame::Person {
        name: "pehcastro".into(),
        commit: SharedString::from(commit.to_owned()),
    }
}

fn agent(turn: u32) -> Blame {
    Blame::Agent {
        name: "go-dev".into(),
        turn,
    }
}

fn blame() -> Vec<BlameLine> {
    [
        ("package turn", person("a1b2c3d")),
        ("", person("a1b2c3d")),
        (
            "func (l *Loop) Step(ctx context.Context) error {",
            person("e4f5a6b"),
        ),
        ("\tif l.budget.Spent() {", agent(4)),
        ("\t\treturn ErrBudget", agent(4)),
        ("\t}", agent(4)),
        ("\treturn l.next(ctx)", person("e4f5a6b")),
        ("}", person("e4f5a6b")),
    ]
    .into_iter()
    .zip(1..)
    .map(|((text, blame), number)| BlameLine {
        number,
        text: text.into(),
        blame,
    })
    .collect()
}

fn inline_blames(id: &'static str, reduced: bool, theme: &Theme) -> Div {
    div()
        .flex()
        .flex_col()
        .gap_2()
        .child(inline_blame(
            (id, 0usize),
            "pehcastro",
            "7 days ago",
            None,
            reduced,
            theme,
        ))
        .child(inline_blame(
            (id, 1usize),
            "you",
            "not committed",
            None,
            reduced,
            theme,
        ))
}

fn changes(cx: &mut Context<Book>) -> Entity<Changes> {
    let file = |path: &'static str, git, staged, agent| ChangedFile {
        path: path.into(),
        git,
        staged,
        agent,
    };
    cx.new(|_| {
        Changes::new(vec![
            file("internal/turn/loop.go", GitStatus::Modified, true, true),
            file("internal/turn/budget.go", GitStatus::Added, true, true),
            file("CHANGELOG.md", GitStatus::Modified, false, false),
            file("internal/turn/ledger.go", GitStatus::Deleted, false, true),
            file("web/scratch.ts", GitStatus::Untracked, false, false),
        ])
    })
}

fn twice(
    title: &'static str,
    theme: &Theme,
    width: f32,
    wide: impl IntoElement,
    narrow: impl IntoElement,
) -> Div {
    page()
        .child(label(title, theme))
        .child(block("board width", theme, div().w(px(width)).child(wide)))
        .child(block("320 px", theme, div().w(px(NARROW)).child(narrow)))
}

fn page() -> Div {
    div().flex().flex_col().gap_3()
}

struct Trees {
    wide: Entity<FileTree>,
    narrow: Entity<FileTree>,
    badged_wide: Entity<FileTree>,
    badged_narrow: Entity<FileTree>,
}

pub(super) struct TreePage {
    trees: Result<Trees, IconThemeError>,
}

impl TreePage {
    pub(super) fn new(cx: &mut Context<Book>) -> Self {
        TreePage {
            trees: IconTheme::material().map(|icons| Trees {
                wide: cx.new(|_| file_tree("tree-wide", &icons)),
                narrow: cx.new(|_| file_tree("tree-narrow", &icons)),
                badged_wide: cx.new(|_| file_tree("tree-badged-wide", &icons).badges()),
                badged_narrow: cx.new(|_| file_tree("tree-badged-narrow", &icons).badges()),
            }),
        }
    }

    pub(super) fn render(&mut self, theme: &Theme, _: &mut Window, _: &mut Context<Book>) -> Div {
        match &self.trees {
            Ok(trees) => page()
                .child(twice(
                    "File tree: click a folder to fold it, click a file to select it, right click for the menu",
                    theme,
                    TREE_WIDTH,
                    panel(trees.wide.clone()),
                    panel(trees.narrow.clone()),
                ))
                .child(twice(
                    "File tree with badges: a git letter beside each changed file, M A U D and ! for a conflict",
                    theme,
                    TREE_WIDTH,
                    panel(trees.badged_wide.clone()),
                    panel(trees.badged_narrow.clone()),
                )),
            Err(error) => page().child(label(error.to_string(), theme)),
        }
    }

    pub(super) fn key(&mut self, _: &str) -> bool {
        false
    }
}

pub(super) struct HistoryPage {
    wide: Entity<History>,
    narrow: Entity<History>,
    held_wide: Entity<History>,
    held_narrow: Entity<History>,
    changes_wide: Entity<Changes>,
    changes_narrow: Entity<Changes>,
}

impl HistoryPage {
    pub(super) fn new(cx: &mut Context<Book>) -> Self {
        HistoryPage {
            wide: history(false, cx),
            narrow: history(false, cx),
            held_wide: history(true, cx),
            held_narrow: history(true, cx),
            changes_wide: changes(cx),
            changes_narrow: changes(cx),
        }
    }

    pub(super) fn render(&mut self, theme: &Theme, _: &mut Window, cx: &mut Context<Book>) -> Div {
        let lines = blame();
        let reduced = reduced_motion(cx);
        page()
            .child(twice(
                "History: commits by day, click one to select it, Branch opens the picker",
                theme,
                HISTORY_WIDTH,
                self.wide.clone(),
                self.narrow.clone(),
            ))
            .child(twice(
                "Branch switch held while ts-dev writes",
                theme,
                HISTORY_WIDTH,
                self.held_wide.clone(),
                self.held_narrow.clone(),
            ))
            .child(twice(
                "Blame gutter: grey for a person and a commit, violet for an agent and a turn",
                theme,
                BLAME_WIDTH,
                blame_gutter(&lines, theme),
                blame_gutter(&lines, theme),
            ))
            .child(twice(
                "Changes: click a row to stage or unstage it",
                theme,
                HISTORY_WIDTH,
                self.changes_wide.clone(),
                self.changes_narrow.clone(),
            ))
            .child(twice(
                "Inline blame: muted, seven columns after the caret line's code, fades in over 120 ms",
                theme,
                BLAME_WIDTH,
                inline_blames("wide", reduced, theme),
                inline_blames("narrow", reduced, theme),
            ))
    }

    pub(super) fn key(&mut self, _: &str) -> bool {
        false
    }
}
