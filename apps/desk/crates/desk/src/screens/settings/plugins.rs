use std::cell::Cell;
use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::rc::Rc;
use std::thread;
use std::time::Instant;

use desk_plugin::{Capability, GRANT_FILE, Grant, Grants, Home, Manifest, Mode};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::caption;
use desk_ui::components::chip::Tone;
use desk_ui::components::form::{segmented, switch_bare};
use desk_ui::components::paint::{glyph, ink, ring, tint};
use desk_ui::components::settings::{SettingRow, Source, page_title, setting_group};
use desk_ui::components::size::{FONT_BODY, FONT_SMALL, T2, T3};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, Background, ClickEvent, Context, Div, FontWeight, ObjectFit, PathPromptOptions,
    Render, SharedString, Window, canvas, div, hsla, img, linear_color_stop, linear_gradient,
    prelude::*, px, rgba,
};

use super::stored::Stored;
use crate::plugins::consent::{self, Pending, Scanned};
use crate::plugins::{self, Plugin};
use crate::screens::frame;

const TAB_STRIP: f32 = 32.0;
const PROBLEM_TEXT: f32 = 12.0;
const ACTIONS_GAP: f32 = 8.0;
const ROW_PAD_Y: f32 = 11.0;
const ROW_RULE: f32 = 0.04;
const ROW_NAME: f32 = 13.5;
const CARD_RADIUS: f32 = 11.0;
const CARD_FILL: f32 = 0.2;
const CARD_RING: f32 = 0.05;
const ROW_TILE: f32 = 30.0;
const ROW_TILE_RADIUS: f32 = 8.0;
const BANNER_HEIGHT: f32 = 128.0;
const BANNER_RADIUS: f32 = 12.0;
const BANNER_PAD: f32 = 16.0;
const BANNER_TILE: f32 = 44.0;
const BANNER_TILE_RADIUS: f32 = 11.0;
const BANNER_TITLE: f32 = 20.0;
const BANNER_INK: u32 = 0xffff_fff0;
const BANNER_SHADE: u32 = 0x0000_0066;
const GRADIENT_ANGLE: f32 = 135.0;
const GRADIENT_FROM: (f32, f32) = (0.55, 0.45);
const GRADIENT_TO: (f32, f32) = (0.6, 0.22);
const GRADIENT_TURN: f32 = 0.12;
const HUES: u16 = 360;
const FNV_OFFSET: u32 = 0x811c_9dc5;
const FNV_PRIME: u32 = 0x0100_0193;
const SECTION_GAP: f32 = 14.0;
const LINE_GAP: f32 = 3.0;
const AGENT_MODES: [Mode; 3] = [Mode::Ask, Mode::Allow, Mode::Deny];
const AGENT_LABELS: [&str; 3] = ["Ask", "Allow", "Deny"];

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
    manifests: HashMap<String, Result<Manifest, String>>,
    stored: Result<Stored, String>,
    grants: Result<Grants, String>,
    painted: Rc<Cell<Option<Instant>>>,
}

impl PluginsPage {
    pub fn new() -> Self {
        PluginsPage {
            shown: None,
            tab: Tab::Consent,
            problem: None,
            consent: None,
            scans: HashMap::new(),
            manifests: HashMap::new(),
            stored: Stored::load(),
            grants: Home::find()
                .and_then(|home| Grants::load(&home))
                .map_err(|error| error.to_string()),
            painted: Rc::new(Cell::new(None)),
        }
    }

    fn manifest(&mut self, plugin: &Plugin) -> Result<Manifest, String> {
        self.manifests
            .entry(plugin.id.clone())
            .or_insert_with(|| consent::manifest_of(plugin))
            .clone()
    }

    fn running(&self, plugin: &Plugin) -> bool {
        plugin.enabled()
            && !self
                .stored
                .as_ref()
                .is_ok_and(|stored| stored.plugin_off(&plugin.id))
    }

    fn switch_plugin(&mut self, plugin: &'static Plugin, cx: &mut Context<Self>) {
        let on = !self.running(plugin);
        let written = self
            .stored
            .as_mut()
            .map_err(|problem| problem.clone())
            .and_then(|stored| {
                stored.set_plugin(&plugin.id, on)?;
                eprintln!(
                    "desk: plugin {} switched {} in {}",
                    plugin.id,
                    if on { "on" } else { "off" },
                    stored.path().display()
                );
                Ok(())
            });
        if let Err(problem) = written {
            return self.fail(problem, cx);
        }
        if on && !plugin.enabled() && plugin.built_in().is_some() {
            return self.landed(consent::enable(plugin), cx);
        }
        self.problem = None;
        cx.notify();
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

    pub fn show(&mut self, shown: Option<SharedString>, opened: Option<Instant>) {
        if opened.is_some() {
            self.painted.set(opened);
        }
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
        let on = self.running(plugin);
        let said = match (plugin.state(), on) {
            (Some(state), _) => state,
            (None, true) => "Built in, on",
            (None, false) => "Built in, off",
        };
        div()
            .flex()
            .flex_none()
            .items_center()
            .gap(px(ACTIONS_GAP))
            .text_size(px(FONT_SMALL))
            .text_color(ink(theme, T3))
            .child(said)
            .child(
                switch_bare(
                    SharedString::from(format!("plugin-switch-{}", plugin.id)),
                    format!("{} on", plugin.name),
                    on,
                    theme,
                )
                .on_click(cx.listener(move |page, _: &ClickEvent, _, cx| {
                    cx.stop_propagation();
                    page.switch_plugin(plugin, cx);
                })),
            )
    }

    fn plugin_row(
        &mut self,
        plugin: &'static Plugin,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Div {
        let about = self
            .manifest(plugin)
            .ok()
            .and_then(|manifest| manifest.description)
            .and_then(|description| description.lines().next().map(str::to_owned))
            .unwrap_or_else(|| plugin.id.clone());
        div()
            .flex()
            .items_center()
            .gap_3()
            .py(px(ROW_PAD_Y))
            .px_4()
            .border_b_1()
            .border_color(ink(theme, ROW_RULE))
            .child(tile(plugin, ROW_TILE, ROW_TILE_RADIUS))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .flex()
                    .flex_col()
                    .child(div().text_size(px(ROW_NAME)).child(plugin.name.clone()))
                    .child(
                        div()
                            .text_size(px(FONT_SMALL))
                            .text_color(ink(theme, T3))
                            .truncate()
                            .child(about),
                    ),
            )
            .child(self.state_control(plugin, theme, cx))
    }

    fn general(&mut self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let folder = Home::find().map_or_else(
            |error| error.to_string(),
            |home| home.plugins.display().to_string(),
        );
        let rows: Vec<Div> = plugins::all()
            .map(|plugin| self.plugin_row(plugin, theme, cx))
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
            .child(
                caption(format!("{} installed", rows.len()), theme)
                    .pt_2p5()
                    .pb_2(),
            )
            .child(
                div()
                    .flex()
                    .flex_col()
                    .mb_2p5()
                    .rounded(px(CARD_RADIUS))
                    .bg(tint(theme.color(ColorToken::Shadow), CARD_FILL))
                    .shadow(vec![ring(ink(theme, CARD_RING))])
                    .children(rows),
            )
    }

    fn about(
        &mut self,
        plugin: &'static Plugin,
        manifest: &Manifest,
        dir: &Path,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Div {
        let picture = manifest
            .banner
            .as_ref()
            .map(|banner| dir.join(banner.as_path()))
            .filter(|path| path.is_file());
        let banner = div()
            .relative()
            .h(px(BANNER_HEIGHT))
            .rounded(px(BANNER_RADIUS))
            .overflow_hidden()
            .bg(gradient(&plugin.id))
            .children(picture.map(|path| {
                img(path)
                    .absolute()
                    .size_full()
                    .object_fit(ObjectFit::Cover)
            }))
            .child(div().absolute().inset_0().bg(linear_gradient(
                0.0,
                linear_color_stop(rgba(BANNER_SHADE), 0.0),
                linear_color_stop(rgba(0), 1.0),
            )))
            .child(
                div()
                    .absolute()
                    .left(px(BANNER_PAD))
                    .bottom(px(BANNER_PAD))
                    .flex()
                    .items_center()
                    .gap_3()
                    .child(tile(plugin, BANNER_TILE, BANNER_TILE_RADIUS))
                    .child(
                        div()
                            .text_size(px(BANNER_TITLE))
                            .font_weight(FontWeight::SEMIBOLD)
                            .text_color(rgba(BANNER_INK))
                            .child(plugin.name.clone()),
                    ),
            );
        let updates = match plugin.built_in() {
            Some(_) => "updates with the desk",
            None => "updates when you install a newer folder",
        };
        let facts = [
            manifest
                .author
                .as_ref()
                .map(|author| format!("by {author}")),
            Some(format!("version {}", manifest.version)),
            Some(manifest.id.to_string()),
            Some(updates.to_owned()),
        ]
        .into_iter()
        .flatten()
        .collect::<Vec<_>>()
        .join(" · ");
        let links = [
            ("Homepage", manifest.homepage.as_ref()),
            ("Repository", manifest.repository.as_ref()),
        ]
        .into_iter()
        .filter_map(|(label, link)| {
            let url = link?.as_str().to_owned();
            Some(
                div()
                    .id(SharedString::from(format!("plugin-link-{label}")))
                    .cursor_pointer()
                    .text_size(px(FONT_SMALL))
                    .text_color(Tone::Link.color(theme))
                    .child(format!("{label}: {url}"))
                    .on_click(cx.listener(move |_, _: &ClickEvent, _, cx| {
                        eprintln!("desk: plugin link opened {url}");
                        cx.open_url(&url);
                    })),
            )
        })
        .collect::<Vec<_>>();
        let risks = consent::risks(manifest);
        let can = if risks.is_empty() {
            vec!["Asks for nothing outside itself.".to_owned()]
        } else {
            risks.into_iter().map(|risk| risk.said).collect()
        };
        let line = |text: String| {
            div()
                .text_size(px(FONT_SMALL))
                .text_color(ink(theme, T2))
                .child(text)
        };
        div()
            .flex()
            .flex_col()
            .gap(px(SECTION_GAP))
            .mb(px(SECTION_GAP))
            .child(banner)
            .children(manifest.description.clone().map(|description| {
                div()
                    .text_size(px(FONT_BODY))
                    .text_color(ink(theme, T2))
                    .child(description)
            }))
            .child(
                div()
                    .flex()
                    .items_center()
                    .justify_between()
                    .gap(px(ACTIONS_GAP))
                    .child(
                        div()
                            .text_size(px(FONT_SMALL))
                            .text_color(ink(theme, T3))
                            .child(facts),
                    )
                    .child(self.state_control(plugin, theme, cx)),
            )
            .children((!links.is_empty()).then(|| {
                div()
                    .flex()
                    .flex_wrap()
                    .gap(px(SECTION_GAP))
                    .children(links)
            }))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .gap(px(LINE_GAP))
                    .child(caption("What it can do", theme))
                    .children(can.into_iter().map(line)),
            )
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
        let found = plugin
            .folder()
            .and_then(|dir| self.manifest(plugin).map(|manifest| (dir, manifest)));
        let (dir, manifest) = match found {
            Ok(found) => found,
            Err(problem) => {
                return page_title(plugin.name.clone(), Some(plugin.id.clone().into()), theme)
                    .child(self.state_control(plugin, theme, cx).mb_3())
                    .child(frame::note(
                        format!("{} is not installed here: {problem}.", plugin.name),
                        theme,
                    ));
            }
        };
        let body = match self.tab {
            Tab::Options => frame::note(format!("{} declares no options.", plugin.name), theme),
            Tab::Consent => {
                let grants = self.consent_tab(plugin, &dir, &manifest, theme, cx);
                let scanned = consent::contains(self.scanned(plugin, cx), theme);
                div().child(grants).child(scanned.mt_3())
            }
            Tab::Agents => self.agents_tab(plugin, &manifest, theme, cx),
            Tab::Activity => activity_tab(plugin, theme),
        };
        div()
            .child(self.about(plugin, &manifest, &dir, theme, cx))
            .child(div().h(px(TAB_STRIP)).mb_3().child(tabs))
            .child(body)
    }

    fn set_agent(
        &mut self,
        plugin: &'static Plugin,
        command: &str,
        mode: Mode,
        cx: &mut Context<Self>,
    ) {
        let set = self.manifest(plugin).and_then(|manifest| {
            let declared = manifest
                .contributes
                .commands
                .iter()
                .find(|c| c.name == command)
                .ok_or_else(|| format!("{} has no command {command}", plugin.id))?;
            let grants = self.grants.as_mut().map_err(|problem| problem.clone())?;
            grants
                .set(&manifest.id, declared, mode)
                .map_err(|error| error.to_string())?;
            eprintln!(
                "desk: plugin {} agent control {command} {} in {}",
                plugin.id,
                Grant::Agent(mode).name(),
                grants.path().display()
            );
            Ok(())
        });
        match set {
            Ok(()) => {
                self.problem = None;
                cx.notify();
            }
            Err(problem) => self.fail(problem, cx),
        }
    }

    fn agents_tab(
        &self,
        plugin: &'static Plugin,
        manifest: &Manifest,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Div {
        let commands = &manifest.contributes.commands;
        if commands.is_empty() {
            return frame::note(
                format!(
                    "{} offers no commands, so no agent can control it.",
                    plugin.name
                ),
                theme,
            );
        }
        let grants = match &self.grants {
            Ok(grants) => grants,
            Err(problem) => return frame::note(problem.clone(), theme),
        };
        let rows = commands
            .iter()
            .map(|command| {
                let control = match grants.of(&manifest.id, command) {
                    Grant::PersonOnly => div()
                        .text_size(px(FONT_SMALL))
                        .text_color(ink(theme, T3))
                        .child("person only"),
                    Grant::Agent(mode) => {
                        let name = command.name.clone();
                        let selected = AGENT_MODES
                            .iter()
                            .position(|held| *held == mode)
                            .unwrap_or_default();
                        let picked = cx.listener(move |page, at: &usize, _, cx| {
                            if let Some(&mode) = AGENT_MODES.get(*at) {
                                page.set_agent(plugin, &name, mode, cx);
                            }
                        });
                        div().child(
                            div()
                                .id(SharedString::from(format!("agent-mode-{}", command.name)))
                                .child(segmented(
                                    "agent-mode",
                                    &AGENT_LABELS,
                                    selected,
                                    theme,
                                    picked,
                                )),
                        )
                    }
                };
                let about = match command.description.as_str() {
                    "" => command.title.clone(),
                    description => format!("{}: {description}", command.title),
                };
                SettingRow {
                    id: SharedString::from(format!("agent-{}", command.name)),
                    name: command.name.clone().into(),
                    about: about.into(),
                    source: Source::Default,
                    control: control.into_any_element(),
                }
            })
            .collect();
        div()
            .child(setting_group("Commands", rows, theme))
            .child(frame::note(
                format!(
                    "An agent runs these through tofu-desk plugin.run. Ask refuses until the desk can ask you in chat, so set Allow to let agents run one. A person only command never runs for an agent. A click in the desk always runs. Written to {}.",
                    grants.path().display()
                ),
                theme,
            ))
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

fn gradient(id: &str) -> Background {
    let hashed = id.bytes().fold(FNV_OFFSET, |hash, byte| {
        (hash ^ u32::from(byte)).wrapping_mul(FNV_PRIME)
    });
    let degree = u16::try_from(hashed % u32::from(HUES)).unwrap_or_default();
    let hue = f32::from(degree) / f32::from(HUES);
    let (from_saturation, from_light) = GRADIENT_FROM;
    let (to_saturation, to_light) = GRADIENT_TO;
    linear_gradient(
        GRADIENT_ANGLE,
        linear_color_stop(hsla(hue, from_saturation, from_light, 1.0), 0.0),
        linear_color_stop(
            hsla((hue + GRADIENT_TURN).fract(), to_saturation, to_light, 1.0),
            1.0,
        ),
    )
}

fn tile(plugin: &Plugin, size: f32, radius: f32) -> Div {
    div()
        .flex_none()
        .size(px(size))
        .rounded(px(radius))
        .flex()
        .items_center()
        .justify_center()
        .bg(gradient(&plugin.id))
        .child(glyph(plugin.glyph, size / 2.0, rgba(BANNER_INK)))
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
        let timed = self.painted.get().is_some().then(|| {
            let unpainted = Rc::clone(&self.painted);
            canvas(
                |_, _, _| {},
                move |_, (), _, _| {
                    if let Some(opened) = unpainted.take() {
                        eprintln!(
                            "desk: settings plugins painted {:.1} ms",
                            opened.elapsed().as_secs_f64() * 1000.0
                        );
                    }
                },
            )
            .absolute()
            .size_0()
        });
        div()
            .relative()
            .child(page)
            .children(timed)
            .children(self.problem.clone().map(|problem| {
                div()
                    .mt_2()
                    .text_size(px(PROBLEM_TEXT))
                    .text_color(theme.color(ColorToken::StatusDanger))
                    .child(problem)
            }))
    }
}
