use std::time::Duration;

use desk_ui::components::term::{TermCard, TermReplay, TermStatus};
use desk_ui::theme::Theme;
use gpui::{Context, Div, Entity, SharedString, Window, div, prelude::*};

use super::Book;
use super::kit::block;

const STREAMED: [&str; 23] = [
    "\x1b[36m=== RUN\x1b[0m   TestLoopStep",
    "\x1b[32m--- PASS:\x1b[0m TestLoopStep \x1b[2m(0.01s)\x1b[0m",
    "\x1b[36m=== RUN\x1b[0m   TestBudget",
    "\x1b[32m--- PASS:\x1b[0m TestBudget \x1b[2m(0.00s)\x1b[0m",
    "\x1b[36m=== RUN\x1b[0m   TestBudgetFloor",
    "    budget_test.go:18: \x1b[33mfloor reached at 3 turns\x1b[0m",
    "\x1b[32m--- PASS:\x1b[0m TestBudgetFloor \x1b[2m(0.02s)\x1b[0m",
    "\x1b[36m=== RUN\x1b[0m   TestLedger",
    "\x1b[36m=== RUN\x1b[0m   TestLedger/append",
    "\x1b[36m=== RUN\x1b[0m   TestLedger/replay",
    "\x1b[32m--- PASS:\x1b[0m TestLedger \x1b[2m(0.04s)\x1b[0m",
    "    \x1b[32m--- PASS:\x1b[0m TestLedger/append \x1b[2m(0.01s)\x1b[0m",
    "    \x1b[32m--- PASS:\x1b[0m TestLedger/replay \x1b[2m(0.03s)\x1b[0m",
    "\x1b[36m=== RUN\x1b[0m   TestContract",
    "\x1b[33m--- SKIP:\x1b[0m TestContract \x1b[2m(0.00s)\x1b[0m",
    "    contract_test.go:12: \x1b[90mno cassette for this platform\x1b[0m",
    "\x1b[36m=== RUN\x1b[0m   TestOwnership",
    "\x1b[32m--- PASS:\x1b[0m TestOwnership \x1b[2m(0.01s)\x1b[0m",
    "\x1b[36m=== RUN\x1b[0m   TestSubAgentBoundary",
    "\x1b[32m--- PASS:\x1b[0m TestSubAgentBoundary \x1b[2m(0.09s)\x1b[0m",
    "\x1b[1;32mPASS\x1b[0m",
    "coverage: \x1b[1m81.4%\x1b[0m of statements",
    "\x1b[32mok\x1b[0m   internal/turn  \x1b[2m0.412s\x1b[0m",
];
const FAILED: [&str; 6] = [
    "\x1b[36m=== RUN\x1b[0m   TestLoopStep",
    "\x1b[1;31m--- FAIL:\x1b[0m TestLoopStep \x1b[2m(0.00s)\x1b[0m",
    "    loop_test.go:41: budget \x1b[31m0\x1b[0m, want \x1b[32m3\x1b[0m",
    "    loop_test.go:44: \x1b[91mstep returned before the tool call\x1b[0m",
    "\x1b[1;31mFAIL\x1b[0m",
    "\x1b[31mFAIL\x1b[0m internal/turn \x1b[2m0.208s\x1b[0m",
];
const FAILED_TOOK: Duration = Duration::from_millis(208);
const LONG_LINES: usize = 400;
const LONG_TOOK: Duration = Duration::from_millis(1730);
const LONG_KINDS: [&str; 4] = [
    "\x1b[32mok\x1b[0m",
    "\x1b[32mok\x1b[0m",
    "\x1b[33mvet\x1b[0m",
    "\x1b[90mskip\x1b[0m",
];

fn lines(text: &[&'static str]) -> Vec<SharedString> {
    text.iter().copied().map(SharedString::from).collect()
}

fn long_line(at: usize) -> SharedString {
    let kind = LONG_KINDS
        .get(at % LONG_KINDS.len())
        .copied()
        .unwrap_or_default();
    SharedString::from(format!(
        "{kind}  \x1b[34mtofu/internal/pkg{at:03}\x1b[0m  \x1b[2mchecked {at} of {LONG_LINES}\x1b[0m"
    ))
}

pub(super) struct ShellPage {
    replay: Entity<TermReplay>,
}

impl ShellPage {
    pub(super) fn new(cx: &mut Context<Book>) -> Self {
        ShellPage {
            replay: cx
                .new(|cx| TermReplay::new("go test -v ./internal/turn", lines(&STREAMED), 0, cx)),
        }
    }

    pub(super) fn render(&mut self, theme: &Theme, _: &mut Window, _: &mut Context<Book>) -> Div {
        let failed = TermCard::new(
            "term-failed",
            "go test ./internal/turn",
            lines(&FAILED),
            TermStatus::Exited {
                code: 1,
                took: FAILED_TOOK,
            },
        );
        let long = TermCard::new(
            "term-long",
            "go vet -v ./...",
            (1..=LONG_LINES).map(long_line).collect(),
            TermStatus::Exited {
                code: 0,
                took: LONG_TOOK,
            },
        );
        div()
            .flex()
            .flex_col()
            .gap_3()
            .child(block(
                "Running: coloured lines stream in and the body follows the tail, then exit 0",
                theme,
                self.replay.clone(),
            ))
            .child(block("Failed: exit 1", theme, failed))
            .child(block(
                "400 lines: the body stays at its capped height and scrolls with the wheel",
                theme,
                long,
            ))
    }

    pub(super) fn key(&mut self, _: &str) -> bool {
        false
    }
}
