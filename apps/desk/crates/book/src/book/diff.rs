use std::rc::Rc;

use desk_ui::components::diff::{DiffCard, FileChange, FileDiff, Highlight, PatchError};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{Context, Div, Window, div, prelude::*, px};

use super::Book;
use super::kit::{block, label};

const COLUMN_WIDTH: f32 = 760.0;
const LEDGER_STEPS: usize = 40;
const LEDGER_EDIT_EVERY: usize = 6;
const LEDGER_EDIT_AT: usize = 2;
const KEYWORDS: [&str; 12] = [
    "package", "import", "func", "return", "if", "for", "range", "var", "type", "struct", "make",
    "append",
];
const CONSTANTS: [&str; 3] = ["nil", "true", "false"];
const OPERATORS: &str = "=+-*/<>!&|:%";

const LOOP_PATCH: &str = "@@ -40,7 +40,8 @@ func (l *Loop) Step
 func (l *Loop) Step(ctx Context) error {
     if l.done {
         return nil
     }
-    budget := l.limit - 1
+    budget := l.limit - l.spent
+    l.trace(\"step\", budget)
     return l.next(ctx, budget)
 }
@@ -90,5 +91,5 @@ func (l *Loop) Close
 func (l *Loop) Close() error {
-    l.shell.Kill(9)
+    l.shell.Stop()
     l.done = true
     return nil
 }";

const BUDGET_PATCH: &str = "@@ -0,0 +1,14 @@
+package turn
+
+import \"errors\"
+
+var ErrOverBudget = errors.New(\"over budget\")
+
+type Budget struct {
+    limit int
+    spent int
+}
+
+func (b *Budget) Left() int {
+    return b.limit - b.spent
+}";

const LIST_PATCH: &str = "@@ -1,11 +0,0 @@
-package models
-
-// List returns every model the catalog knows.
-func List() []Model {
-    out := make([]Model, 0, len(catalog))
-    for _, model := range catalog {
-        out = append(out, model)
-    }
-    return out
-}
-";

fn ledger_patch() -> String {
    let mut body = vec![
        " package turn".to_owned(),
        " ".to_owned(),
        " import \"errors\"".to_owned(),
        " ".to_owned(),
    ];
    for step in 0..LEDGER_STEPS {
        let name = format!("Step{step:02}");
        let edited = step % LEDGER_EDIT_EVERY == LEDGER_EDIT_AT;
        body.push(format!(" func (l *Ledger) {name}(cost int) error {{"));
        if edited {
            body.push("-    if cost > l.limit {".to_owned());
            body.push("+    if cost > l.limit-l.spent {".to_owned());
        } else {
            body.push("     if cost > l.limit {".to_owned());
        }
        body.push("         return errors.New(\"over budget\")".to_owned());
        body.push("     }".to_owned());
        if edited {
            body.push(format!("+    l.trace(\"{name}\", cost)"));
        }
        body.push("     l.spent += cost".to_owned());
        body.push("     return nil".to_owned());
        body.push(" }".to_owned());
        body.push(" ".to_owned());
    }
    let count = |kept: char| {
        body.iter()
            .filter(|line| line.starts_with(' ') || line.starts_with(kept))
            .count()
    };
    format!(
        "@@ -1,{} +1,{} @@ package turn\n{}",
        count('-'),
        count('+'),
        body.join("\n")
    )
}

fn word_token(word: &str, called: bool) -> ColorToken {
    match word.chars().next() {
        _ if KEYWORDS.contains(&word) => ColorToken::SyntaxKeyword,
        _ if CONSTANTS.contains(&word) => ColorToken::SyntaxConstant,
        Some(first) if first.is_ascii_digit() => ColorToken::SyntaxNumber,
        _ if called => ColorToken::SyntaxFunction,
        Some(first) if first.is_uppercase() => ColorToken::SyntaxType,
        _ => ColorToken::SyntaxVariable,
    }
}

fn go_syntax(text: &str) -> Highlight {
    let mut spans = Vec::new();
    let mut at = 0;
    while let Some(rest) = text.get(at..) {
        let Some(first) = rest.chars().next() else {
            break;
        };
        let word = |c: char| c.is_alphanumeric() || c == '_';
        let (len, token) = if rest.starts_with("//") {
            (rest.len(), Some(ColorToken::SyntaxComment))
        } else if first == '"' || first == '`' {
            let close = rest.get(1..).and_then(|inner| inner.find(first));
            (
                close.map_or(rest.len(), |end| end + 2),
                Some(ColorToken::SyntaxString),
            )
        } else if word(first) {
            let len = rest.find(|c: char| !word(c)).unwrap_or(rest.len());
            let called = rest.get(len..).is_some_and(|after| after.starts_with('('));
            let token = word_token(rest.get(..len).unwrap_or_default(), called);
            (len, Some(token))
        } else if OPERATORS.contains(first) {
            (first.len_utf8(), Some(ColorToken::SyntaxOperator))
        } else if first.is_whitespace() {
            (first.len_utf8(), None)
        } else {
            (first.len_utf8(), Some(ColorToken::SyntaxPunctuation))
        };
        if let Some(token) = token {
            spans.push((at..at + len, token));
        }
        at += len;
    }
    spans
}

fn samples() -> Result<Vec<Rc<FileDiff>>, PatchError> {
    let ledger = ledger_patch();
    [
        ("internal/turn/loop.go", FileChange::Modified, LOOP_PATCH),
        ("internal/turn/ledger.go", FileChange::Modified, &ledger),
        ("internal/turn/budget.go", FileChange::Added, BUDGET_PATCH),
        ("library/models/list.go", FileChange::Deleted, LIST_PATCH),
    ]
    .into_iter()
    .map(|(path, change, patch)| FileDiff::parse(path, change, patch, go_syntax).map(Rc::new))
    .collect()
}

pub(super) struct DiffPage {
    cards: Result<Vec<Rc<FileDiff>>, PatchError>,
}

impl DiffPage {
    pub(super) fn new(_cx: &mut Context<Book>) -> Self {
        DiffPage { cards: samples() }
    }

    pub(super) fn render(
        &mut self,
        theme: &Theme,
        _window: &mut Window,
        _cx: &mut Context<Book>,
    ) -> Div {
        let column = div().flex().flex_col().gap_3().max_w(px(COLUMN_WIDTH));
        let column = match &self.cards {
            Ok(cards) => column.children(
                cards
                    .iter()
                    .enumerate()
                    .map(|(ix, diff)| DiffCard::new(("diff-card", ix), diff.clone())),
            ),
            Err(problem) => column.child(label(format!("sample patch: {problem}"), theme)),
        };
        div().flex().flex_col().gap_3().child(block(
            "An edit, a long file that scrolls and folds, a new file and a deleted one. Click a header to collapse it",
            theme,
            column,
        ))
    }

    pub(super) fn key(&mut self, _key: &str) -> bool {
        false
    }
}
