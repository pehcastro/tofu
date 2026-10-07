use std::collections::BTreeMap;
use std::fmt;

use gpui::{Rgba, SharedString, rgba};
use serde_json::{Map, Value};

macro_rules! tokens {
    ($kind:ident { $($variant:ident => $path:literal),+ $(,)? }) => {
        #[derive(Clone, Copy, Debug, PartialEq, Eq)]
        pub enum $kind {
            $($variant),+
        }

        impl $kind {
            pub const ALL: [$kind; [$($path),+].len()] = [$($kind::$variant),+];

            pub fn path(self) -> &'static str {
                match self {
                    $($kind::$variant => $path),+
                }
            }
        }
    };
}

tokens!(ColorToken {
    SurfaceWindow => "surface.window",
    ToastFill => "toast.fill",
    ToastBadge => "toast.badge",
    CardsOuterFill => "cards.outer.fill",
    CardsInnerFill => "cards.inner.fill",
    CardsInnerSheen => "cards.inner.sheen",
    TextBase => "text.base",
    TextMuted => "text.muted",
    TextName => "text.name",
    TextIcon => "text.icon",
    TextTab => "text.tab",
    TextStatus => "text.status",
    TextCaption => "text.caption",
    TextStrong => "text.strong",
    StatusAccent => "status.accent",
    StatusLive => "status.live",
    StatusWarn => "status.warn",
    StatusDanger => "status.danger",
    GitModified => "git.modified",
    GitAdded => "git.added",
    GitDeleted => "git.deleted",
    GitUntracked => "git.untracked",
    AgentsGoDev => "agents.go-dev",
    AgentsTsDev => "agents.ts-dev",
    AgentsExplore => "agents.explore",
    AgentsResearch => "agents.research",
    AgentsQa => "agents.qa",
    AgentsBrowser => "agents.browser",
    AgentsPyDev => "agents.py-dev",
    SyntaxKeyword => "syntax.keyword",
    SyntaxString => "syntax.string",
    SyntaxNumber => "syntax.number",
    SyntaxComment => "syntax.comment",
    SyntaxFunction => "syntax.function",
    SyntaxType => "syntax.type",
    SyntaxVariable => "syntax.variable",
    SyntaxConstant => "syntax.constant",
    SyntaxOperator => "syntax.operator",
    SyntaxPunctuation => "syntax.punctuation",
    AnsiBlack => "terminal.ansi.normal.black",
    AnsiRed => "terminal.ansi.normal.red",
    AnsiGreen => "terminal.ansi.normal.green",
    AnsiYellow => "terminal.ansi.normal.yellow",
    AnsiBlue => "terminal.ansi.normal.blue",
    AnsiMagenta => "terminal.ansi.normal.magenta",
    AnsiCyan => "terminal.ansi.normal.cyan",
    AnsiWhite => "terminal.ansi.normal.white",
    AnsiBrightBlack => "terminal.ansi.bright.black",
    AnsiBrightRed => "terminal.ansi.bright.red",
    AnsiBrightGreen => "terminal.ansi.bright.green",
    AnsiBrightYellow => "terminal.ansi.bright.yellow",
    AnsiBrightBlue => "terminal.ansi.bright.blue",
    AnsiBrightMagenta => "terminal.ansi.bright.magenta",
    AnsiBrightCyan => "terminal.ansi.bright.cyan",
    AnsiBrightWhite => "terminal.ansi.bright.white",
    DiffAddedLine => "diff.added.line",
    DiffAddedWord => "diff.added.word",
    DiffRemovedLine => "diff.removed.line",
    DiffRemovedWord => "diff.removed.word",
    FocusRing => "focus.ring",
    Selection => "selection",
    Scrollbar => "scrollbar",
    Shadow => "shadow",
    StateHover => "state.hover",
    StateActive => "state.active",
    StateDisabled => "state.disabled",
    StateCaptionHover => "state.caption_hover",
    StateCloseHover => "state.close_hover",
    AccountFrom => "account.from",
    AccountTo => "account.to",
    CardsInnerFillEnd => "cards.inner.fill_end",
    CardsInnerShadow => "cards.inner.shadow",
    CardsDots => "cards.dots",
    TabsFill => "tabs.fill",
    TabsHover => "tabs.hover",
    ButtonFill => "button.fill",
    ButtonPrimary => "button.primary",
    ButtonPrimaryText => "button.primary_text",
    FieldFill => "field.fill",
    SegmentedFill => "segmented.fill",
    SegmentedOn => "segmented.on",
    SwitchOn => "switch.on",
    SwitchOff => "switch.off",
    SwitchThumb => "switch.thumb",
    AvatarRing => "avatar.ring",
    Trace => "trace",
    MentionText => "mention.text",
    ChatYou => "chat.you",
    ChatCalls => "chat.calls",
    Separator => "separator",
    StateCloseBox => "state.close_box",
});

tokens!(BorderToken {
    CardsOuter => "cards.outer.border",
    CardsInner => "cards.inner.border",
    Toast => "toast.border",
    Tabs => "tabs.border",
});

tokens!(NumberToken {
    CardsOuterRadius => "cards.outer.radius",
    CardsOuterGap => "cards.outer.gap",
    CardsInnerRadius => "cards.inner.radius",
    ShapeBlur => "shape.blur",
    MotionFast => "motion.fast",
    MotionBase => "motion.base",
    MotionEnter => "motion.enter",
    MotionExit => "motion.exit",
    MotionTile => "motion.tile",
    MotionSpin => "motion.spin",
});

tokens!(FlagToken {
    CardsInnerHighlight => "cards.inner.highlight",
});

tokens!(WordToken {
    ShapeFont => "shape.font",
    ShapeMono => "shape.mono",
});

tokens!(ChoiceToken {
    Mode => "mode",
    ShapeDensity => "shape.density",
    TabsActive => "tabs.active",
});

impl ChoiceToken {
    fn options(self) -> &'static [&'static str] {
        match self {
            ChoiceToken::Mode => &["dark", "light"],
            ChoiceToken::ShapeDensity => &["compact", "comfortable"],
            ChoiceToken::TabsActive => &["inner", "outer"],
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Mode {
    Light,
    Dark,
}

impl Mode {
    pub const ALL: &'static [Self] = &[Mode::Light, Mode::Dark];

    pub fn label(self) -> &'static str {
        match self {
            Mode::Light => "light",
            Mode::Dark => "dark",
        }
    }

    fn other(self) -> Self {
        match self {
            Mode::Light => Mode::Dark,
            Mode::Dark => Mode::Light,
        }
    }

    fn strip(self, path: &str) -> Option<&str> {
        path.strip_prefix(self.label())?.strip_prefix('.')
    }
}

fn paints(path: &str) -> bool {
    ColorToken::ALL.iter().any(|token| token.path() == path)
        || BorderToken::ALL.iter().any(|token| token.path() == path)
}

fn known(path: &str) -> bool {
    if let Some(rest) = Mode::ALL.iter().find_map(|mode| mode.strip(path)) {
        return paints(rest);
    }
    paints(path)
        || NumberToken::ALL.iter().any(|token| token.path() == path)
        || FlagToken::ALL.iter().any(|token| token.path() == path)
        || WordToken::ALL.iter().any(|token| token.path() == path)
        || ChoiceToken::ALL.iter().any(|token| token.path() == path)
}

#[derive(Clone, Debug)]
pub struct Theme {
    mode: Mode,
    colors: Vec<Rgba>,
    borders: Vec<Option<Rgba>>,
    numbers: Vec<f32>,
    flags: Vec<bool>,
    words: Vec<SharedString>,
    choices: Vec<&'static str>,
}

impl Theme {
    pub fn mode(&self) -> Mode {
        self.mode
    }

    pub fn color(&self, token: ColorToken) -> Rgba {
        self.colors[token as usize]
    }

    pub fn border(&self, token: BorderToken) -> Option<Rgba> {
        self.borders[token as usize]
    }

    pub fn number(&self, token: NumberToken) -> f32 {
        self.numbers[token as usize]
    }

    pub fn flag(&self, token: FlagToken) -> bool {
        self.flags[token as usize]
    }

    pub fn word(&self, token: WordToken) -> SharedString {
        self.words[token as usize].clone()
    }

    pub fn choice(&self, token: ChoiceToken) -> &'static str {
        self.choices[token as usize]
    }
}

#[derive(Clone, Debug, PartialEq)]
pub enum Problem {
    Unreadable(String),
    Syntax(String),
    NotAnObject,
    NotAName(String),
    ExtendsLoop(Vec<String>),
    UnknownToken,
    SetTwice,
    UnknownReference(String),
    ReferenceLoop(Vec<String>),
    Expected(&'static str, String),
    Unset,
    Watch(String),
    MissingMode,
    OnlyIn(Mode),
}

impl fmt::Display for Problem {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Problem::Unreadable(error) => write!(f, "cannot read {error}"),
            Problem::Syntax(error) => write!(f, "not JSON, {error}"),
            Problem::NotAnObject => write!(f, "not a JSON object"),
            Problem::NotAName(name) => write!(f, "{name} is not a theme file name"),
            Problem::ExtendsLoop(trail) => write!(f, "loops: {}", trail.join(" -> ")),
            Problem::UnknownToken => write!(f, "no such token"),
            Problem::SetTwice => write!(f, "set twice"),
            Problem::UnknownReference(target) => write!(f, "refers to @{target}, which is unset"),
            Problem::ReferenceLoop(trail) => write!(f, "reference loop: {}", trail.join(" -> ")),
            Problem::Expected(what, got) => write!(f, "expects {what}, got {got}"),
            Problem::Unset => write!(f, "unset"),
            Problem::Watch(error) => write!(f, "cannot watch, {error}"),
            Problem::MissingMode => write!(f, "missing, a theme carries both light and dark"),
            Problem::OnlyIn(mode) => write!(f, "missing, set only in {}", mode.label()),
        }
    }
}

#[derive(Clone, Debug, PartialEq)]
pub struct ThemeError {
    file: String,
    path: String,
    problem: Problem,
}

impl ThemeError {
    pub fn new(file: &str, path: &str, problem: Problem) -> Self {
        ThemeError {
            file: file.to_owned(),
            path: path.to_owned(),
            problem,
        }
    }
}

impl fmt::Display for ThemeError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self.path.as_str() {
            "" => write!(f, "{}: {}", self.file, self.problem),
            path => write!(f, "{} {path}: {}", self.file, self.problem),
        }
    }
}

impl std::error::Error for ThemeError {}

pub struct Layer {
    file: String,
    tokens: BTreeMap<String, Value>,
}

pub struct ParsedLayer {
    pub layer: Layer,
    pub extends: Option<String>,
}

pub fn parse_layer(
    file: &str,
    text: &str,
    problems: &mut Vec<ThemeError>,
) -> Result<ParsedLayer, ThemeError> {
    let fail = |path: &str, problem| ThemeError::new(file, path, problem);
    let root: Value =
        serde_json::from_str(text).map_err(|error| fail("", Problem::Syntax(error.to_string())))?;
    let Value::Object(mut root) = root else {
        return Err(fail("", Problem::NotAnObject));
    };
    let extends = match root.remove("extends") {
        None => None,
        Some(Value::String(name)) => Some(name),
        Some(other) => return Err(fail("extends", Problem::NotAName(other.to_string()))),
    };
    let name = root.remove("name");
    if let Some(name) = name.as_ref().filter(|name| !name.is_string()) {
        problems.push(fail(
            "name",
            Problem::Expected("a string", name.to_string()),
        ));
    }
    let mut tokens = BTreeMap::new();
    let mut pending: Vec<(String, Map<String, Value>)> = vec![(String::new(), root)];
    while let Some((prefix, object)) = pending.pop() {
        for (key, value) in object {
            let path = format!("{prefix}{key}");
            match value {
                Value::Object(inner) => pending.push((format!("{path}."), inner)),
                _ if !known(&path) => problems.push(fail(&path, Problem::UnknownToken)),
                leaf => {
                    if tokens.insert(path.clone(), leaf).is_some() {
                        problems.push(fail(&path, Problem::SetTwice));
                    }
                }
            }
        }
    }
    let carries = |mode: Mode| tokens.keys().any(|path| mode.strip(path).is_some());
    for &mode in Mode::ALL {
        let other = mode.other();
        if !carries(mode) {
            if name.is_some() || carries(other) {
                problems.push(fail(mode.label(), Problem::MissingMode));
            }
            continue;
        }
        for path in tokens.keys().filter_map(|path| other.strip(path)) {
            let mine = format!("{}.{path}", mode.label());
            if !tokens.contains_key(&mine) {
                problems.push(fail(&mine, Problem::OnlyIn(other)));
            }
        }
    }
    Ok(ParsedLayer {
        layer: Layer {
            file: file.to_owned(),
            tokens,
        },
        extends,
    })
}

pub fn build(layers: &[Layer], problems: &mut Vec<ThemeError>) -> Result<Theme, ThemeError> {
    let chain = Chain {
        layers,
        mode: Mode::Dark,
    };
    let mode = chain.resolve(ChoiceToken::Mode.path(), mode_of, problems)?;
    build_mode(layers, mode, problems)
}

pub fn build_mode(
    layers: &[Layer],
    mode: Mode,
    problems: &mut Vec<ThemeError>,
) -> Result<Theme, ThemeError> {
    let chain = Chain { layers, mode };
    let mut choices: Vec<&'static str> = ChoiceToken::ALL
        .iter()
        .map(|token| chain.resolve(token.path(), |value| choice(*token, value), problems))
        .collect::<Result<_, _>>()?;
    choices[ChoiceToken::Mode as usize] = mode.label();
    Ok(Theme {
        mode,
        choices,
        colors: ColorToken::ALL
            .iter()
            .map(|token| chain.resolve(token.path(), color, problems))
            .collect::<Result<_, _>>()?,
        borders: BorderToken::ALL
            .iter()
            .map(|token| chain.resolve(token.path(), border, problems))
            .collect::<Result<_, _>>()?,
        numbers: NumberToken::ALL
            .iter()
            .map(|token| chain.resolve(token.path(), number, problems))
            .collect::<Result<_, _>>()?,
        flags: FlagToken::ALL
            .iter()
            .map(|token| chain.resolve(token.path(), flag, problems))
            .collect::<Result<_, _>>()?,
        words: WordToken::ALL
            .iter()
            .map(|token| chain.resolve(token.path(), word, problems))
            .collect::<Result<_, _>>()?,
    })
}

struct Chain<'a> {
    layers: &'a [Layer],
    mode: Mode,
}

impl<'a> Chain<'a> {
    fn get(&self, layer: &'a Layer, path: &str) -> Option<&'a Value> {
        let moded = format!("{}.{path}", self.mode.label());
        layer.tokens.get(&moded).or_else(|| layer.tokens.get(path))
    }

    fn resolve<T>(
        &self,
        path: &str,
        parse: impl Fn(&Value) -> Result<T, Problem>,
        problems: &mut Vec<ThemeError>,
    ) -> Result<T, ThemeError> {
        for layer in self.layers {
            let Some(value) = self.get(layer, path) else {
                continue;
            };
            match self.follow(path, value).and_then(&parse) {
                Ok(parsed) => return Ok(parsed),
                Err(problem) => problems.push(ThemeError::new(&layer.file, path, problem)),
            }
        }
        let last = self.layers.last().map_or("", |layer| layer.file.as_str());
        Err(ThemeError::new(last, path, Problem::Unset))
    }

    fn follow(&self, path: &str, mut value: &'a Value) -> Result<&'a Value, Problem> {
        let mut trail = vec![path.to_owned()];
        while let Some(target) = value.as_str().and_then(|text| text.strip_prefix('@')) {
            let looped = trail.iter().any(|seen| seen == target);
            trail.push(target.to_owned());
            if looped {
                return Err(Problem::ReferenceLoop(trail));
            }
            value = self
                .layers
                .iter()
                .find_map(|layer| self.get(layer, target))
                .ok_or_else(|| Problem::UnknownReference(target.to_owned()))?;
        }
        Ok(value)
    }
}

fn expected(what: &'static str, value: &Value) -> Problem {
    Problem::Expected(what, value.to_string())
}

const A_COLOR: &str = "a color such as #rrggbb, #rrggbbaa or rgba(r, g, b, a)";

fn color(value: &Value) -> Result<Rgba, Problem> {
    value
        .as_str()
        .and_then(css_color)
        .ok_or_else(|| expected(A_COLOR, value))
}

fn border(value: &Value) -> Result<Option<Rgba>, Problem> {
    match value.as_str() {
        Some("none") => Ok(None),
        _ => color(value)
            .map(Some)
            .map_err(|_| expected("none or a color", value)),
    }
}

fn number(value: &Value) -> Result<f32, Problem> {
    value
        .as_f64()
        .map(|number| number as f32)
        .filter(|number| number.is_finite() && *number >= 0.0)
        .ok_or_else(|| expected("a number, zero or more", value))
}

fn mode_of(value: &Value) -> Result<Mode, Problem> {
    Mode::ALL
        .iter()
        .copied()
        .find(|mode| value.as_str() == Some(mode.label()))
        .ok_or_else(|| expected("light or dark", value))
}

fn flag(value: &Value) -> Result<bool, Problem> {
    value
        .as_bool()
        .ok_or_else(|| expected("true or false", value))
}

fn word(value: &Value) -> Result<SharedString, Problem> {
    value
        .as_str()
        .filter(|text| !text.is_empty())
        .map(|text| SharedString::from(text.to_owned()))
        .ok_or_else(|| expected("a name", value))
}

fn choice(token: ChoiceToken, value: &Value) -> Result<&'static str, Problem> {
    let options = token.options();
    options
        .iter()
        .find(|option| value.as_str() == Some(**option))
        .copied()
        .ok_or_else(|| {
            Problem::Expected(
                "one of the listed options",
                format!("{value} (options: {})", options.join(", ")),
            )
        })
}

fn css_color(text: &str) -> Option<Rgba> {
    if let Some(hex) = text.strip_prefix('#') {
        return hex_color(hex);
    }
    let (function, rest) = text.split_once('(')?;
    let channels: Vec<f32> = rest
        .strip_suffix(')')?
        .split(',')
        .map(|part| part.trim().parse().ok())
        .collect::<Option<_>>()?;
    let byte = |channel: f32| (0.0..=255.0).contains(&channel).then_some(channel / 255.0);
    let (red, green, blue, alpha) = match (function.trim(), channels.as_slice()) {
        ("rgb", &[red, green, blue]) => (red, green, blue, 1.0),
        ("rgba", &[red, green, blue, alpha]) => (red, green, blue, alpha),
        _ => return None,
    };
    let alpha = (0.0..=1.0).contains(&alpha).then_some(alpha)?;
    Some(Rgba::new(byte(red)?, byte(green)?, byte(blue)?, alpha))
}

fn hex_color(hex: &str) -> Option<Rgba> {
    if !hex.chars().all(|digit| digit.is_ascii_hexdigit()) {
        return None;
    }
    let long: String = match hex.len() {
        3 | 4 => hex.chars().flat_map(|digit| [digit, digit]).collect(),
        6 | 8 => hex.to_owned(),
        _ => return None,
    };
    let opaque = if long.len() == 6 { long + "ff" } else { long };
    u32::from_str_radix(&opaque, 16).ok().map(rgba)
}
