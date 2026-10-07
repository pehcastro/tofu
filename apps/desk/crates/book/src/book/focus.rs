use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::form::{segmented_with_focus, switch};
use desk_ui::components::tabs::{Tab, TabEvent, TabMark, TabStrip};
use desk_ui::theme::Theme;
use gpui::{
    App, ClickEvent, Context, Div, FocusHandle, KeyDownEvent, MouseButton, MouseDownEvent,
    SharedString, Window, div, prelude::*,
};

use super::Book;
use super::kit::{block, label, named, spread};

const CHOICES: [&str; 3] = ["Table", "Feed", "Map"];
const TABS: [&str; 3] = ["Chat", "Files", "Shells"];
const TAB_ROOM: usize = 3;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Control {
    Button,
    Segmented,
    Switch,
    Tabs,
}

impl Control {
    const ALL: [Control; 4] = [
        Control::Button,
        Control::Segmented,
        Control::Switch,
        Control::Tabs,
    ];

    fn name(self) -> &'static str {
        match self {
            Control::Button => "Button",
            Control::Segmented => "Segmented",
            Control::Switch => "Switch",
            Control::Tabs => "Tabs",
        }
    }
}

pub(super) struct FocusPage {
    handles: [FocusHandle; 4],
    choice: usize,
    on: bool,
    active: usize,
    last: SharedString,
}

fn by(event: &ClickEvent) -> &'static str {
    if event.is_keyboard() {
        "the keyboard"
    } else {
        "the mouse"
    }
}

fn focus_on_press(focus: FocusHandle) -> impl Fn(&MouseDownEvent, &mut Window, &mut App) {
    move |_, window, cx| window.focus(&focus, cx)
}

fn told(this: &mut Book, last: String, cx: &mut Context<Book>) {
    this.focus_page.last = last.into();
    cx.notify();
}

impl FocusPage {
    pub(super) fn new(cx: &mut Context<Book>) -> Self {
        FocusPage {
            handles: Control::ALL.map(|_| cx.focus_handle().tab_stop(true)),
            choice: 0,
            on: false,
            active: 0,
            last: "nothing yet".into(),
        }
    }

    fn handle(&self, control: Control) -> &FocusHandle {
        let [button, segmented, switch, tabs] = &self.handles;
        match control {
            Control::Button => button,
            Control::Segmented => segmented,
            Control::Switch => switch,
            Control::Tabs => tabs,
        }
    }

    fn focused(&self, window: &Window) -> Option<Control> {
        Control::ALL
            .into_iter()
            .find(|control| self.handle(*control).is_focused(window))
    }

    pub(super) fn key(
        &mut self,
        event: &KeyDownEvent,
        window: &mut Window,
        cx: &mut Context<Book>,
    ) -> bool {
        let keys = &event.keystroke;
        if keys.key != "tab" {
            return false;
        }
        match (self.focused(window), keys.modifiers.shift) {
            (None, false) => window.focus(self.handle(Control::Button), cx),
            (None, true) => window.focus(self.handle(Control::Tabs), cx),
            (Some(_), false) => window.focus_next(cx),
            (Some(_), true) => window.focus_prev(cx),
        }
        true
    }

    fn readout(&self, window: &Window) -> String {
        match self.focused(window) {
            None => "Focus: not in this row. Tab enters it.".to_owned(),
            Some(control) if window.last_input_was_keyboard() => {
                format!("Focus: {}, from the keyboard, ring shown", control.name())
            }
            Some(control) => format!("Focus: {}, from the mouse, no ring", control.name()),
        }
    }

    pub(super) fn render(&self, theme: &Theme, window: &Window, cx: &mut Context<Book>) -> Div {
        let tabs: Vec<Tab> = TABS
            .iter()
            .map(|name| Tab {
                label: (*name).into(),
                icon: None,
                count: None,
                mark: TabMark::Pinned,
            })
            .collect();
        let tabs_focus = self.handle(Control::Tabs).clone();
        let strip = TabStrip::connected(
            "focus-tabs",
            &tabs,
            self.active,
            TAB_ROOM,
            theme,
            cx.listener(|this, event: &TabEvent, _, cx| {
                if let TabEvent::Select(at) = event {
                    this.focus_page.active = *at;
                    let name = TABS.get(*at).copied().unwrap_or_default();
                    told(this, format!("Tabs: {name} selected"), cx);
                }
            }),
        )
        .focus(&tabs_focus)
        .on_press(move |_, _, window, cx| window.focus(&tabs_focus, cx));
        let button = button(
            "focus-button",
            "Reset layout",
            None,
            ButtonKind::Plain,
            theme,
        )
        .track_focus(self.handle(Control::Button))
        .on_mouse_down(
            MouseButton::Left,
            focus_on_press(self.handle(Control::Button).clone()),
        )
        .on_click(cx.listener(|this, event: &ClickEvent, _, cx| {
            told(this, format!("Button: pressed by {}", by(event)), cx);
        }));
        let segmented = segmented_with_focus(
            self.handle(Control::Segmented),
            "focus-segmented",
            &CHOICES,
            self.choice,
            theme,
            cx.listener(|this, at: &usize, _, cx| {
                this.focus_page.choice = *at;
                let name = CHOICES.get(*at).copied().unwrap_or_default();
                told(this, format!("Segmented: {name} chosen"), cx);
            }),
        )
        .on_mouse_down(
            MouseButton::Left,
            focus_on_press(self.handle(Control::Segmented).clone()),
        );
        let switch = switch("focus-switch", "Notifications", self.on, theme)
            .track_focus(self.handle(Control::Switch))
            .on_mouse_down(
                MouseButton::Left,
                focus_on_press(self.handle(Control::Switch).clone()),
            )
            .on_click(cx.listener(|this, event: &ClickEvent, _, cx| {
                this.focus_page.on = !this.focus_page.on;
                let state = if this.focus_page.on { "on" } else { "off" };
                told(this, format!("Switch: turned {state} by {}", by(event)), cx);
            }));
        let row = spread(theme)
            .gap_6()
            .items_start()
            .child(named("button", theme, button))
            .child(named("segmented", theme, segmented))
            .child(named("switch", theme, switch))
            .child(named("tabs", theme, div().w_64().child(strip)));
        div().flex().flex_col().gap_3().child(block(
            "Tab moves between the four, Enter and Space press, Left and Right move",
            theme,
            div()
                .flex()
                .flex_col()
                .gap_3()
                .child(row)
                .child(label(self.readout(window), theme))
                .child(label(format!("Last: {}", self.last), theme)),
        ))
    }
}
