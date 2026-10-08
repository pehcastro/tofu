use desk_ui::components::status_bar::{Status, StatusBar, StatusPick};
use desk_ui::theme::ThemeError;
use gpui::Context;

use crate::desk::Desk;

fn problem_line(problems: &[ThemeError]) -> Option<String> {
    let first = problems.first()?;
    Some(match problems.len() {
        1 => first.to_string(),
        count => format!("{first} (and {} more)", count - 1),
    })
}

pub fn status_bar(problems: &[ThemeError], status: Status, cx: &mut Context<Desk>) -> StatusBar {
    let status = Status {
        problem: problem_line(problems).map(Into::into),
        ..status
    };
    StatusBar::new(
        "status-bar",
        status,
        cx.listener(|desk, pick: &StatusPick, window, cx| match pick {
            StatusPick::AllProviders => desk.open_usage(window, cx),
            StatusPick::Branch
            | StatusPick::Session
            | StatusPick::Context
            | StatusPick::Classifier
            | StatusPick::Cron => {
                eprintln!(
                    "desk: status {pick:?} opens a screen still on fixtures, so it stays inert"
                )
            }
        }),
    )
}
