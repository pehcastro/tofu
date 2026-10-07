use std::cell::Cell;
use std::ops::Range;
use std::rc::Rc;

use desk_core::buffer::Buffer;
use desk_core::syntax::{Kind, Language, Syntax, SyntaxError};
use desk_ui::components::chip::GitStatus;
use desk_ui::components::code::{CodeLine, code_view};
use desk_ui::components::history::{
    Blame, BlameLine, ChangedFile, Changes, Commit, Day, History, blame_gutter,
};
use desk_ui::components::tree::{FileTree, IconTheme, IconThemeError, TreeNode};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    Context, Div, Entity, IntoElement, SharedString, UniformListScrollHandle, Window, div,
    prelude::*, px,
};

use super::Book;
use super::kit::{block, label, spread};

const TREE_WIDTH: f32 = 282.0;
const PANEL_PAD_Y: f32 = 8.0;
const PANEL_PAD_X: f32 = 4.0;
const HISTORY_WIDTH: f32 = 460.0;
const BLAME_WIDTH: f32 = 640.0;
const NARROW: f32 = 320.0;
const WRITER: &str = "ts-dev";
const HELD_BRANCH: &str = "feat/sort-order";
const CODE_WIDTH: f32 = 720.0;
const CODE_HEIGHT: f32 = 706.0;
const SWATCH: f32 = 10.0;
const CODE_PATH: &str = "ui/src/components/form.rs";
const CODE_TEXT: &str = include_str!("../../../ui/src/components/form.rs");
const KINDS: [Kind; 10] = [
    Kind::Keyword,
    Kind::String,
    Kind::Number,
    Kind::Comment,
    Kind::Function,
    Kind::Type,
    Kind::Variable,
    Kind::Constant,
    Kind::Operator,
    Kind::Punctuation,
];

fn syntax_token(kind: Kind) -> ColorToken {
    match kind {
        Kind::Keyword => ColorToken::SyntaxKeyword,
        Kind::String => ColorToken::SyntaxString,
        Kind::Number => ColorToken::SyntaxNumber,
        Kind::Comment => ColorToken::SyntaxComment,
        Kind::Function => ColorToken::SyntaxFunction,
        Kind::Type => ColorToken::SyntaxType,
        Kind::Variable => ColorToken::SyntaxVariable,
        Kind::Constant => ColorToken::SyntaxConstant,
        Kind::Operator => ColorToken::SyntaxOperator,
        Kind::Punctuation => ColorToken::SyntaxPunctuation,
    }
}

fn byte_range(text: &str, chars: Range<usize>) -> Range<usize> {
    let byte = |char: usize| {
        text.char_indices()
            .nth(char)
            .map_or(text.len(), |(at, _)| at)
    };
    byte(chars.start)..byte(chars.end)
}

fn coloured(text: &str) -> Result<Vec<CodeLine>, SyntaxError> {
    let buffer = Buffer::from_text(text);
    let syntax = Syntax::new(Language::Rust, &buffer)?;
    let count = buffer.line_count();
    let mut lines: Vec<CodeLine> = (0..count)
        .map(|row| CodeLine {
            text: buffer.line(row).unwrap_or_default().into(),
            runs: Vec::new(),
        })
        .collect();
    for span in syntax.spans(0..count) {
        let mut at = span.chars.start;
        while at < span.chars.end {
            let Ok(row) = buffer.char_to_line(at) else {
                break;
            };
            let Ok(start) = buffer.line_to_char(row) else {
                break;
            };
            let end = buffer
                .line_to_char(row + 1)
                .map_or(span.chars.end, |next| next.min(span.chars.end));
            if let Some(line) = lines.get_mut(row) {
                let bytes = byte_range(&line.text, at - start..end - start);
                if !bytes.is_empty() {
                    line.runs.push((bytes, syntax_token(span.kind)));
                }
            }
            at = end;
        }
    }
    Ok(lines)
}

struct CodeSample {
    lines: Rc<[CodeLine]>,
    widest: usize,
    scroll: UniformListScrollHandle,
    built: Rc<Cell<usize>>,
    shown: usize,
}

impl CodeSample {
    fn new() -> Result<Self, SyntaxError> {
        let lines: Rc<[CodeLine]> = coloured(CODE_TEXT)?.into();
        let widest = lines
            .iter()
            .enumerate()
            .max_by_key(|(_, line)| line.text.chars().count())
            .map_or(0, |(row, _)| row);
        Ok(CodeSample {
            lines,
            widest,
            scroll: UniformListScrollHandle::new(),
            built: Rc::default(),
            shown: 0,
        })
    }

    fn render(&mut self, theme: &Theme, window: &mut Window) -> Div {
        let built = self.built.replace(0);
        if built != self.shown {
            self.shown = built;
            window.request_animation_frame();
        }
        let (lines, counter) = (self.lines.clone(), self.built.clone());
        let view = code_view("code-sample", lines.len(), &self.scroll, move |row| {
            counter.set(counter.get() + 1);
            lines.get(row).cloned().unwrap_or_default()
        })
        .widest(self.widest);
        let legend = spread(theme).children(KINDS.map(|kind| {
            div()
                .flex()
                .items_center()
                .gap_1()
                .child(
                    div()
                        .size(px(SWATCH))
                        .rounded_sm()
                        .bg(theme.color(syntax_token(kind))),
                )
                .child(label(kind.name(), theme))
        }));
        page()
            .child(label(
                format!(
                    "Code view: {CODE_PATH}, {} lines coloured by desk_core::syntax, rows built last frame {}",
                    self.lines.len(),
                    self.shown
                ),
                theme,
            ))
            .child(legend)
            .child(block(
                "board width",
                theme,
                div().w(px(CODE_WIDTH)).h(px(CODE_HEIGHT)).child(view),
            ))
    }
}

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

fn file_tree(id: &'static str, icons: &IconTheme, cx: &mut Context<Book>) -> Entity<FileTree> {
    cx.new(|_| {
        FileTree::new(id, tree(), icons)
            .closed(".github")
            .closed("cmd")
            .closed("migrations")
            .closed("node_modules")
            .selected("notes/notes.go")
    })
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

pub(super) struct TreePage {
    trees: Result<(Entity<FileTree>, Entity<FileTree>), IconThemeError>,
    code: Result<CodeSample, SyntaxError>,
}

impl TreePage {
    pub(super) fn new(cx: &mut Context<Book>) -> Self {
        TreePage {
            trees: IconTheme::material().map(|icons| {
                (
                    file_tree("tree-wide", &icons, cx),
                    file_tree("tree-narrow", &icons, cx),
                )
            }),
            code: CodeSample::new(),
        }
    }

    pub(super) fn render(
        &mut self,
        theme: &Theme,
        window: &mut Window,
        _: &mut Context<Book>,
    ) -> Div {
        let trees = match &self.trees {
            Ok((wide, narrow)) => twice(
                "File tree: click a folder to fold it, click a file to select it, right click for the menu",
                theme,
                TREE_WIDTH,
                panel(wide.clone()),
                panel(narrow.clone()),
            ),
            Err(error) => label(error.to_string(), theme),
        };
        let code = match &mut self.code {
            Ok(code) => code.render(theme, window),
            Err(error) => label(error.to_string(), theme),
        };
        page().child(trees).child(code)
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

    pub(super) fn render(&mut self, theme: &Theme, _: &mut Window, _: &mut Context<Book>) -> Div {
        let lines = blame();
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
    }

    pub(super) fn key(&mut self, _: &str) -> bool {
        false
    }
}
