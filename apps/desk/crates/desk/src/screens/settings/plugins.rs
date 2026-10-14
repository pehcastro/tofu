use std::cell::Cell;
use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::rc::Rc;
use std::thread;
use std::time::Instant;

use desk_plugin::{Capability, GRANT_FILE, Home, Mode};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::form::switch_bare;
use desk_ui::components::settings::{SettingRow, Source, page_title, setting_group};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, ClickEvent, Context, Div, PathPromptOptions, Render, SharedString, Window, div,
    prelude::*, px,
};

use crate::plugins::consent::{self, Pending, Scanned};
use crate::plugins::{self, Plugin};
use crate::screens::frame;

const TAB_STRIP: f32 = 32.0;
const PROBLEM_TEXT: f32 = 12.0;
const ACTIONS_GAP: f32 = 8.0;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Tab {
    Options,
    Consent,
    Agents,
    Activity,
}

const TABS: [(Tab, &str); 4] = [
    (Tab::Options, "Options"),
    (Tab::Consent, "Consent"),
    (Tab::Agents, "Agent control"),
    (Tab::Activity, "Activity"),
];

struct Consenting {
    pending: Pending,
    open: bool,
    painted: Rc<Cell<Option<Instant>>>,
}

pub struct PluginsPage {
    shown: Option<SharedString>,
    tab: Tab,
    problem: Option<SharedString>,
    consent: Option<Consenting>,
    scans: HashMap<String, Scanned>,
}

impl PluginsPage {
    pub fn new() -> Self {
        PluginsPage {
            shown: None,
            tab: Tab::Consent,
            problem: None,
            consent: None,
            scans: HashMap::new(),
        }
    }

    fn scanned(&mut self, plugin: &'static Plugin, cx: &mut Context<Self>) -> &Scanned {
        self.scans.entry(plugin.id.clone()).or_insert_with(|| {
            cx.spawn(async move |page, cx| {
                let (sent, reply) = flume::bounded(1);
                thread::spawn(move || {
                    if sent.send(consent::scan_of(plugin)).is_err() {
                        eprintln!("desk: plugins: settings closed before the scan finished");
                    }
                });
                let Ok(scanned) = reply.recv_async().await else {
                    return eprintln!("desk: plugins: the scan stopped without an answer");
                };
                let landed = page.update(cx, |page, cx| {
                    page.scans.insert(plugin.id.clone(), scanned);
                    cx.notify();
                });
                if landed.is_err() {
                    eprintln!("desk: plugins: settings closed before the scan landed");
                }
            })
            .detach();
            Scanned::Waiting
        })
    }

    pub fn show(&mut self, shown: Option<SharedString>) {
        if self.shown != shown {
            self.problem = None;
            self.shown = shown;
        }
    }

    fn fail(&mut self, problem: String, cx: &mut Context<Self>) {
        eprintln!("desk: plugins: {problem}");
        self.problem = Some(problem.into());
        cx.notify();
    }

    fn install(&mut self, cx: &mut Context<Self>) {
        eprintln!("desk: plugins: install from folder asked");
        let asked = cx.prompt_for_paths(PathPromptOptions {
            files: false,
            directories: true,
            multiple: false,
            prompt: Some("Install plugin".into()),
        });
        cx.spawn(async move |page, cx| {
            let folder = match asked.await {
                Ok(Ok(Some(mut picked))) => picked.pop(),
                Ok(Ok(None)) => None,
                Ok(Err(error)) => {
                    eprintln!("desk: plugins: the folder dialog failed: {error}");
                    None
                }
                Err(_) => {
                    eprintln!("desk: plugins: the folder dialog closed without an answer");
                    None
                }
            };
            let Some(folder) = folder else {
                return eprintln!("desk: plugins: no folder was picked");
            };
            if page
                .update(cx, |page, cx| page.install_from(folder, cx))
                .is_err()
            {
                eprintln!("desk: plugins: settings closed before the folder was picked");
            }
        })
        .detach();
    }

    pub fn install_from(&mut self, folder: PathBuf, cx: &mut Context<Self>) {
        eprintln!("desk: plugins: install from {}", folder.display());
        cx.spawn(async move |page, cx| {
            let (sent, reply) = flume::bounded(1);
            thread::spawn(move || {
                if sent.send(consent::install(&folder)).is_err() {
                    eprintln!("desk: plugins: settings closed before the install finished");
                }
            });
            let Ok(installed) = reply.recv_async().await else {
                return eprintln!("desk: plugins: the install stopped without an answer");
            };
            if page
                .update(cx, |page, cx| page.landed(installed, cx))
                .is_err()
            {
                eprintln!("desk: plugins: settings closed before the install landed");
            }
        })
        .detach();
    }

    fn landed(&mut self, installed: Result<Pending, String>, cx: &mut Context<Self>) {
        match installed {
            Ok(pending) => {
                self.problem = None;
                self.consent = Some(Consenting {
                    painted: Rc::new(Cell::new(Some(pending.asked))),
                    pending,
                    open: true,
                });
                cx.notify();
            }
            Err(problem) => self.fail(problem, cx),
        }
    }

    fn decide(&mut self, allow: bool, cx: &mut Context<Self>) {
        let Some(consenting) = self.consent.as_mut().filter(|consenting| consenting.open) else {
            return;
        };
        consenting.open = false;
        let decided = if allow {
            consent::allow(&consenting.pending)
        } else {
            consent::cancel(&consenting.pending)
        };
        match decided {
            Ok(()) => cx.notify(),
            Err(problem) => self.fail(problem, cx),
        }
    }

    pub fn consent_sheet(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let theme = ActiveTheme::theme(cx);
        let allow = cx.listener(|page, _: &(), _, cx| page.decide(true, cx));
        let cancel = cx.listener(|page, _: &(), _, cx| page.decide(false, cx));
        let consenting = self.consent.as_ref()?;
        Some(
            consent::sheet(
                &consenting.pending,
                consenting.open,
                &consenting.painted,
                &theme,
            )
            .on_confirm(move |window, cx| allow(&(), window, cx))
            .on_cancel(move |window, cx| cancel(&(), window, cx))
            .into_any_element(),
        )
    }

    fn toggle(&mut self, plugin: &'static Plugin, capability: Capability, cx: &mut Context<Self>) {
        let id = plugin.id.as_str();
        let flipped = plugin.folder().and_then(|dir| {
            let mut modes = consent::modes(id, &dir)?;
            let mode = match modes.get(&capability) {
                Some(Mode::Allow) => Mode::Deny,
                Some(Mode::Ask | Mode::Deny) | None => Mode::Allow,
            };
            modes.insert(capability, mode);
            consent::grant(id, &dir, modes)?;
            eprintln!(
                "desk: plugin {id} consent {capability} {}",
                consent::mode_name(mode)
            );
            Ok(())
        });
        match flipped {
            Ok(()) => cx.notify(),
            Err(problem) => self.fail(problem, cx),
        }
    }

    fn state_control(&self, plugin: &'static Plugin, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let said = match (plugin.state(), plugin.enabled()) {
            (Some(state), _) => state,
            (None, true) => "Built in, enabled",
            (None, false) => "Not enabled",
        };
        let enable = (plugin.built_in().is_some() && !plugin.enabled()).then(|| {
            button(
                SharedString::from(format!("plugin-enable-{}", plugin.id)),
                "Enable",
                None,
                ButtonKind::Primary,
                theme,
            )
            .on_click(cx.listener(move |page, _: &ClickEvent, _, cx| {
                cx.stop_propagation();
                page.landed(consent::enable(plugin), cx);
            }))
        });
        div()
            .flex()
            .items_center()
            .gap(px(ACTIONS_GAP))
            .child(said)
            .children(enable)
    }

    fn general(&mut self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let folder = Home::find().map_or_else(
            |error| error.to_string(),
            |home| home.plugins.display().to_string(),
        );
        let rows: Vec<SettingRow> = plugins::all()
            .map(|plugin| SettingRow {
                id: SharedString::from(format!("plugin-{}", plugin.id)),
                name: plugin.name.clone().into(),
                about: plugin.id.clone().into(),
                source: Source::Global,
                control: self.state_control(plugin, theme, cx).into_any_element(),
            })
            .collect();
        let install = button(
            "plugins-install",
            "Install from folder…",
            None,
            ButtonKind::Primary,
            theme,
        )
        .on_click(cx.listener(|page, _: &ClickEvent, _, cx| page.install(cx)));
        div()
            .child(page_title(
                "Plugins",
                Some("How plugins behave inside tofu. Each installed plugin has its own page in the list on the left.".into()),
                theme,
            ))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(ACTIONS_GAP))
                    .child(install)
                    .child(frame::note(format!("Plugins folder: {folder}"), theme)),
            )
            .child(setting_group(
                format!("{} installed", rows.len()),
                rows,
                theme,
            ))
    }

    fn plugin(&mut self, id: &str, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let Some(plugin) = plugins::find(id) else {
            return page_title(
                id.to_owned(),
                Some("This plugin is no longer installed.".into()),
                theme,
            );
        };
        let tabs = frame::tile_tabs("plugin-tabs", &TABS, self.tab, theme, cx, |page, tab, _| {
            eprintln!("desk: settings plugin tab {tab:?}");
            page.tab = tab;
        });
        let body = match plugin
            .folder()
            .and_then(|dir| consent::manifest_of(plugin).map(|manifest| (dir, manifest)))
        {
            Err(problem) => frame::note(
                format!("{} is not installed here: {problem}.", plugin.name),
                theme,
            ),
            Ok((dir, manifest)) => match self.tab {
                Tab::Options => frame::note(format!("{} declares no options.", plugin.name), theme),
                Tab::Consent => {
                    let grants = self.consent_tab(plugin, &dir, &manifest, theme, cx);
                    let scanned = consent::contains(self.scanned(plugin, cx), theme);
                    div().child(grants).child(scanned.mt_3())
                }
                Tab::Agents => agents_tab(plugin, &manifest, theme),
                Tab::Activity => activity_tab(plugin, theme),
            },
        };
        div()
            .child(page_title(
                plugin.name.clone(),
                Some(plugin.id.clone().into()),
                theme,
            ))
            .child(self.state_control(plugin, theme, cx).mb_3())
            .child(div().h(px(TAB_STRIP)).mb_3().child(tabs))
            .child(body)
    }

    fn consent_tab(
        &mut self,
        plugin: &'static Plugin,
        dir: &Path,
        manifest: &desk_plugin::Manifest,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Div {
        let modes = match consent::modes(&plugin.id, dir) {
            Ok(modes) => modes,
            Err(problem) => return frame::note(problem, theme),
        };
        let risks = consent::risks(manifest);
        if risks.is_empty() {
            return frame::note(
                format!("{} asks for nothing outside itself.", plugin.name),
                theme,
            );
        }
        let mut groups: Vec<(consent::Group, Vec<SettingRow>)> = Vec::new();
        for risk in risks {
            let capability = risk.capability;
            let on = modes.get(&capability) == Some(&Mode::Allow);
            let row = SettingRow {
                id: SharedString::from(format!("grant-{capability}")),
                name: risk.said.into(),
                about: risk.risk.into(),
                source: Source::Global,
                control: switch_bare(
                    SharedString::from(format!("grant-switch-{capability}")),
                    capability.name(),
                    on,
                    theme,
                )
                .on_click(cx.listener(move |page, _: &ClickEvent, _, cx| {
                    cx.stop_propagation();
                    page.toggle(plugin, capability, cx);
                }))
                .into_any_element(),
            };
            match groups.last_mut() {
                Some((group, rows)) if *group == risk.group => rows.push(row),
                _ => groups.push((risk.group, vec![row])),
            }
        }
        let applies = match plugin.state() {
            None => "A change applies to the running plugin at once.".to_owned(),
            Some(_) => "A running WASM plugin reads a change the next time it loads.".to_owned(),
        };
        div()
            .children(
                groups
                    .into_iter()
                    .map(|(group, rows)| setting_group(group.label(), rows, theme)),
            )
            .child(frame::note(
                format!("Written to {}. {applies}", dir.join(GRANT_FILE).display()),
                theme,
            ))
    }
}

fn agents_tab(plugin: &Plugin, manifest: &desk_plugin::Manifest, theme: &Theme) -> Div {
    let commands = consent::agents(manifest);
    if commands.is_empty() {
        return frame::note(
            format!(
                "{} offers agents no commands, so no agent can control it.",
                plugin.name
            ),
            theme,
        );
    }
    let rows = commands
        .into_iter()
        .map(|command| SettingRow {
            id: SharedString::from(format!("agent-{}", command.name)),
            name: command.name.clone().into(),
            about: command.description.clone().into(),
            source: Source::Default,
            control: div().child("agents may call").into_any_element(),
        })
        .collect();
    setting_group("Commands agents may call", rows, theme)
}

fn activity_tab(plugin: &Plugin, theme: &Theme) -> Div {
    let calls = consent::activity(&plugin.id);
    let recorded = match plugin.state() {
        None => format!(
            "The last {} host calls this run, newest first.",
            consent::ACTIVITY_KEPT
        ),
        Some(_) => "A WASM plugin's host calls are not recorded here yet.".to_owned(),
    };
    if calls.is_empty() {
        return frame::note(
            format!("No host calls since the desk started. {recorded}"),
            theme,
        );
    }
    let rows = calls
        .into_iter()
        .enumerate()
        .map(|(at, call)| {
            let verdict = if call.allowed { "allowed" } else { "denied" };
            SettingRow {
                id: SharedString::from(format!("call-{at}")),
                name: format!("{verdict} {}", call.capability).into(),
                about: call.what.into(),
                source: Source::Default,
                control: div()
                    .when(!call.allowed, |time| {
                        time.text_color(theme.color(ColorToken::StatusWarn))
                    })
                    .child(call.at)
                    .into_any_element(),
            }
        })
        .collect();
    div()
        .child(setting_group("Host calls", rows, theme))
        .child(frame::note(recorded, theme))
}

impl Render for PluginsPage {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let page = match self.shown.clone() {
            None => self.general(&theme, cx),
            Some(id) => self.plugin(&id, &theme, cx),
        };
        div()
            .child(page)
            .children(self.problem.clone().map(|problem| {
                div()
                    .mt_2()
                    .text_size(px(PROBLEM_TEXT))
                    .text_color(theme.color(ColorToken::StatusDanger))
                    .child(problem)
            }))
    }
}
