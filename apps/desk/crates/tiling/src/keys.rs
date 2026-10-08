#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Action {
    NewWorkspace,
    GoToWorkspace,
    SendToNewWorkspace,
    Zoom,
    Close,
    Even,
    Reset,
    Lock,
    Undo,
    Cancel,
    CloseTab,
    ReopenTab,
    NextTab,
    PrevTab,
    Split,
}

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub struct Mods {
    pub ctrl: bool,
    pub alt: bool,
    pub shift: bool,
}

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Key {
    Named(&'static str),
    Digit,
}

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub struct Shortcut {
    pub action: Action,
    pub mods: Mods,
    pub key: Key,
    pub keys: &'static str,
    pub label: &'static str,
}

const PLAIN: Mods = Mods {
    ctrl: false,
    alt: false,
    shift: false,
};
const CTRL: Mods = Mods {
    ctrl: true,
    ..PLAIN
};
const CTRL_ALT: Mods = Mods { alt: true, ..CTRL };
const ALT: Mods = Mods { alt: true, ..PLAIN };
const CTRL_SHIFT: Mods = Mods {
    shift: true,
    ..CTRL
};

pub const SHORTCUTS: [Shortcut; 15] = [
    Shortcut {
        action: Action::NewWorkspace,
        mods: CTRL,
        key: Key::Named("t"),
        keys: "Ctrl T",
        label: "new workspace",
    },
    Shortcut {
        action: Action::GoToWorkspace,
        mods: CTRL,
        key: Key::Digit,
        keys: "Ctrl 1-9",
        label: "go to workspace",
    },
    Shortcut {
        action: Action::SendToNewWorkspace,
        mods: CTRL_SHIFT,
        key: Key::Named("enter"),
        keys: "Ctrl Shift Enter",
        label: "move to a new workspace",
    },
    Shortcut {
        action: Action::Zoom,
        mods: CTRL_ALT,
        key: Key::Named("enter"),
        keys: "Ctrl Alt Enter",
        label: "zoom or unzoom",
    },
    Shortcut {
        action: Action::Close,
        mods: ALT,
        key: Key::Named("w"),
        keys: "Alt W",
        label: "close tile",
    },
    Shortcut {
        action: Action::Even,
        mods: CTRL_ALT,
        key: Key::Named("e"),
        keys: "Ctrl Alt E",
        label: "even",
    },
    Shortcut {
        action: Action::Reset,
        mods: CTRL_ALT,
        key: Key::Named("r"),
        keys: "Ctrl Alt R",
        label: "reset",
    },
    Shortcut {
        action: Action::Lock,
        mods: CTRL_ALT,
        key: Key::Named("l"),
        keys: "Ctrl Alt L",
        label: "lock or unlock",
    },
    Shortcut {
        action: Action::Undo,
        mods: CTRL,
        key: Key::Named("z"),
        keys: "Ctrl Z",
        label: "undo",
    },
    Shortcut {
        action: Action::Cancel,
        mods: PLAIN,
        key: Key::Named("escape"),
        keys: "Escape",
        label: "cancel a drag or unzoom",
    },
    Shortcut {
        action: Action::CloseTab,
        mods: CTRL,
        key: Key::Named("w"),
        keys: "Ctrl W",
        label: "close tab",
    },
    Shortcut {
        action: Action::ReopenTab,
        mods: CTRL_SHIFT,
        key: Key::Named("t"),
        keys: "Ctrl Shift T",
        label: "reopen closed tab",
    },
    Shortcut {
        action: Action::NextTab,
        mods: CTRL,
        key: Key::Named("tab"),
        keys: "Ctrl Tab",
        label: "next tab",
    },
    Shortcut {
        action: Action::PrevTab,
        mods: CTRL_SHIFT,
        key: Key::Named("tab"),
        keys: "Ctrl Shift Tab",
        label: "previous tab",
    },
    Shortcut {
        action: Action::Split,
        mods: CTRL,
        key: Key::Named("\\"),
        keys: "Ctrl \\",
        label: "split tile",
    },
];

pub fn action(mods: Mods, key: &str) -> Option<Action> {
    let digit = key.len() == 1 && key.bytes().all(|byte| (b'1'..=b'9').contains(&byte));
    SHORTCUTS
        .iter()
        .find(|shortcut| {
            shortcut.mods == mods
                && match shortcut.key {
                    Key::Named(name) => name == key,
                    Key::Digit => digit,
                }
        })
        .map(|shortcut| shortcut.action)
}
