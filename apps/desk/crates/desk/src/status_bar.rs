#[cfg(feature = "screen-work")]
use std::time::SystemTime;

#[cfg(feature = "screen-work")]
use crate::modules::chat::Chat;
#[cfg(feature = "screen-work")]
use desk_core::protocol::{Accounts, DecisionMade, Role};
#[cfg(feature = "screen-work")]
use desk_core::query::Answer;
#[cfg(feature = "screen-work")]
use desk_ui::components::classifier_card::{Bound, LATEST, Ruling, Rulings, count_kinds};
#[cfg(feature = "screen-work")]
use desk_ui::components::cron_card::Job;
use desk_ui::components::status_bar::{Status, StatusBar, StatusPick};
use desk_ui::theme::ThemeError;
#[cfg(feature = "screen-work")]
use gpui::App;
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
            StatusPick::AllProviders => desk.open_limits(window, cx),
            StatusPick::SignIn(source) => desk.sign_in(source, cx),
            StatusPick::Context | StatusPick::Classifier | StatusPick::Cron => {
                desk.open_card(pick, true, window, cx)
            }
            StatusPick::Branch | StatusPick::Session => {
                eprintln!(
                    "desk: status {pick:?} opens a screen still on fixtures, so it stays inert"
                )
            }
        }),
    )
}

#[cfg(feature = "screen-work")]
pub fn rulings(chat: &Chat, cx: &App) -> Rulings {
    let store = chat.store().read(cx);
    let decisions: &[DecisionMade] = chat
        .shown_id()
        .and_then(|id| store.sessions.get(&id))
        .map_or(&[], |session| &session.decisions);
    let shown = chat.decisions_shown(cx);
    let newest_first = || decisions.iter().rev().chain(chat.past_decisions());
    Rulings {
        bound: bound(chat.accounts()),
        total: shown.count,
        capped: shown.capped,
        kinds: count_kinds(newest_first().map(|decision| &decision.verdict)),
        latest: newest_first()
            .take(LATEST)
            .map(|decision| Ruling {
                verdict: decision.verdict.clone(),
                point: decision.point.clone().into(),
                tool: decision.tool.clone().into(),
                at: chrono::DateTime::parse_from_rfc3339(&decision.at)
                    .map(|at| {
                        at.with_timezone(&chrono::Local)
                            .format("%H:%M:%S")
                            .to_string()
                    })
                    .unwrap_or_else(|_| decision.at.clone())
                    .into(),
            })
            .collect(),
    }
}

#[cfg(feature = "screen-work")]
fn bound(accounts: &Answer<Accounts>) -> Bound {
    let Some(read) = &accounts.read else {
        return Bound::Reading;
    };
    let key = read
        .value
        .keys
        .iter()
        .find(|key| key.role == Role::Classifier)
        .map(|key| match &key.model {
            Some(model) if !model.contains('/') => format!("{}/{model}", key.provider),
            Some(model) => model.clone(),
            None => key.provider.clone(),
        });
    let subscription = || {
        read.value
            .subscriptions
            .iter()
            .find(|source| source.role == Role::Classifier)
            .map(|source| source.source.clone())
    };
    key.or_else(subscription)
        .map_or(Bound::Unbound, |slug| Bound::Model(slug.into()))
}

#[cfg(feature = "screen-work")]
pub fn cron_jobs(chat: &Chat, cx: &App) -> Vec<Job> {
    let store = chat.store().read(cx);
    let Some(cron) = chat
        .shown_id()
        .and_then(|id| store.sessions.get(&id))
        .and_then(|session| session.cron.as_ref())
    else {
        return Vec::new();
    };
    cron.jobs
        .iter()
        .map(|job| Job {
            id: job.id.clone().into(),
            schedule: job.schedule.clone().into(),
            prompt: job.prompt.clone().into(),
            paused: job.paused,
            next: job
                .next
                .as_deref()
                .and_then(|at| chrono::DateTime::parse_from_rfc3339(at).ok())
                .map(SystemTime::from),
            ended: job.ended.clone().map(Into::into),
        })
        .collect()
}
