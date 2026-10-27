use std::cell::RefCell;
use std::ops::Range;
use std::rc::Rc;

use desk_core::control::TELL_BADGE;
use desk_ui::component::icon;
use desk_ui::components::avatar::{
    Agent as Worker, AgentKind, AgentStatus, AvatarSize, Face, PersonSize, avatar, person_avatar,
};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{
    Header, caption, dots, header_action, inner_card, shell, strip_tab,
};
use desk_ui::components::chat::{Agent, AgentMark, Block, agent_row, document, markdown, you};
use desk_ui::components::chip::{GitStatus, agent_pill, flat_chip, git_name, mention, mono};
use desk_ui::components::diff::{DiffCard, FileChange, FileDiff};
use desk_ui::components::dot_chip::dot_chip;
use desk_ui::components::form::{input, segmented, switch_bare};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::history::inline_blame;
use desk_ui::components::overlay::{MenuItem, menu, toast};
use desk_ui::components::paint::{glyph, ring};
use desk_ui::components::tooltip::overlay_surface;
use desk_ui::icon::Icon;
use desk_ui::metrics::ICON_SMALL;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, BorderStyle, Bounds, ContentMask, Div, Edges, FontWeight, HighlightStyle, Pixels,
    SharedString, StyledText, canvas, div, prelude::*, px, quad, rgb_to_hsla, transparent_black,
};

use super::{CARD_PAD, heading};
use crate::screens::frame;

const SECTION_GAP: f32 = 12.0;
const LIT_SPREAD: f32 = 2.0;
const ROW_RADIUS: f32 = 4.0;
const CAPTION_BUTTON: f32 = 28.0;
const STRIP_HEIGHT: f32 = 36.0;
const SMALL_TEXT: f32 = 12.0;
const MENU_ROW_WIDTH: f32 = 180.0;
const PATCH: &str =
    "@@ -1,3 +1,3 @@\n fn accent() -> Color {\n-    Color::blue()\n+    Color::red()\n }\n";
const CODE: &str = "Warm the **accent** a little, as the [theme docs](https://tofu.dev/theme) say:\n\n```rust\n/// The accent links are painted with.\n#[derive(Clone, Copy)]\npub enum Accent { Warm, Cold(u8) }\n\nfn mix(base: Color, by: f32) -> Color {\n    let label = format!(\"{by:.2}\\n\");\n    'outer: for step in 0..3 { if step > 1 && true { break 'outer; } }\n    Color::rgb(0xff, 0x7a, 0x86).with(self.alpha, &label)\n}\n```\n\n```tsx\nconst warm = /warm|cold/g;\nexport const Swatch = () => <div className=\"swatch\">{`#${hex}`}</div>;\n```\n";

const WINDOW: &[ColorToken] = &[
    ColorToken::SurfaceWindow,
    ColorToken::TextName,
    ColorToken::TextTab,
    ColorToken::TextMuted,
    ColorToken::TextCaption,
    ColorToken::StateCaptionHover,
    ColorToken::StateCloseHover,
    ColorToken::TextStatus,
];
const CHAT: &[ColorToken] = &[
    ColorToken::CardsOuterFill,
    ColorToken::CardsInnerFill,
    ColorToken::CardsInnerFillEnd,
    ColorToken::CardsInnerSheen,
    ColorToken::CardsInnerShadow,
    ColorToken::CardsDots,
    ColorToken::TextIcon,
    ColorToken::TextBase,
    ColorToken::TextStrong,
    ColorToken::AvatarRing,
    ColorToken::AccountFrom,
    ColorToken::AccountTo,
    ColorToken::AgentsGoDev,
    ColorToken::AgentsTsDev,
    ColorToken::AgentsExplore,
    ColorToken::AgentsResearch,
    ColorToken::AgentsQa,
    ColorToken::AgentsBrowser,
    ColorToken::AgentsPyDev,
    ColorToken::AgentsRustDev,
    ColorToken::AgentsRustDesigner,
];
const SYNTAX: &[ColorToken] = &[
    ColorToken::SyntaxKeyword,
    ColorToken::SyntaxString,
    ColorToken::SyntaxNumber,
    ColorToken::SyntaxComment,
    ColorToken::SyntaxFunction,
    ColorToken::SyntaxType,
    ColorToken::SyntaxVariable,
    ColorToken::SyntaxConstant,
    ColorToken::SyntaxOperator,
    ColorToken::SyntaxPunctuation,
    ColorToken::SyntaxAttribute,
    ColorToken::SyntaxBoolean,
    ColorToken::SyntaxCommentDoc,
    ColorToken::SyntaxConstructor,
    ColorToken::SyntaxEmbedded,
    ColorToken::SyntaxEmphasis,
    ColorToken::SyntaxEmphasisStrong,
    ColorToken::SyntaxEnum,
    ColorToken::SyntaxLabel,
    ColorToken::SyntaxLinkText,
    ColorToken::SyntaxLinkUri,
    ColorToken::SyntaxNamespace,
    ColorToken::SyntaxPreproc,
    ColorToken::SyntaxProperty,
    ColorToken::SyntaxPunctuationBracket,
    ColorToken::SyntaxPunctuationDelimiter,
    ColorToken::SyntaxPunctuationListMarker,
    ColorToken::SyntaxPunctuationMarkup,
    ColorToken::SyntaxPunctuationSpecial,
    ColorToken::SyntaxTag,
    ColorToken::SyntaxSelector,
    ColorToken::SyntaxSelectorPseudo,
    ColorToken::SyntaxStringEscape,
    ColorToken::SyntaxStringRegex,
    ColorToken::SyntaxStringSpecial,
    ColorToken::SyntaxStringSpecialSymbol,
    ColorToken::SyntaxTextLiteral,
    ColorToken::SyntaxTitle,
    ColorToken::SyntaxVariableParameter,
    ColorToken::SyntaxVariableSpecial,
    ColorToken::SyntaxVariant,
];
const DIFF: &[ColorToken] = &[
    ColorToken::DiffAddedLine,
    ColorToken::DiffAddedWord,
    ColorToken::DiffRemovedLine,
    ColorToken::DiffRemovedWord,
    ColorToken::ChatCalls,
    ColorToken::Separator,
];
const FILES: &[ColorToken] = &[
    ColorToken::GitModified,
    ColorToken::GitAdded,
    ColorToken::GitDeleted,
    ColorToken::GitUntracked,
    ColorToken::Trace,
    ColorToken::MentionText,
];
const CONTROLS: &[ColorToken] = &[
    ColorToken::ButtonFill,
    ColorToken::ButtonPrimary,
    ColorToken::ButtonPrimaryText,
    ColorToken::StateActive,
    ColorToken::FieldFill,
    ColorToken::FocusRing,
    ColorToken::SwitchOn,
    ColorToken::SwitchOff,
    ColorToken::SwitchThumb,
    ColorToken::SegmentedFill,
    ColorToken::SegmentedOn,
    ColorToken::TabsFill,
];
const OVERLAYS: &[ColorToken] = &[
    ColorToken::ToastFill,
    ColorToken::Shadow,
    ColorToken::StateHover,
    ColorToken::StatusDanger,
];
const STATUS: &[ColorToken] = &[
    ColorToken::StatusAccent,
    ColorToken::StatusLive,
    ColorToken::StatusWarn,
    ColorToken::StatusDanger,
];
const TERMINAL_TOKENS: &[ColorToken] = &[
    ColorToken::Selection,
    ColorToken::AnsiBlack,
    ColorToken::AnsiRed,
    ColorToken::AnsiGreen,
    ColorToken::AnsiYellow,
    ColorToken::AnsiBlue,
    ColorToken::AnsiMagenta,
    ColorToken::AnsiCyan,
    ColorToken::AnsiWhite,
    ColorToken::AnsiBrightBlack,
    ColorToken::AnsiBrightRed,
    ColorToken::AnsiBrightGreen,
    ColorToken::AnsiBrightYellow,
    ColorToken::AnsiBrightBlue,
    ColorToken::AnsiBrightMagenta,
    ColorToken::AnsiBrightCyan,
    ColorToken::AnsiBrightWhite,
];
const CARDS_INNER: &[ColorToken] = &[
    ColorToken::CardsInnerFill,
    ColorToken::CardsInnerFillEnd,
    ColorToken::CardsInnerSheen,
    ColorToken::CardsInnerShadow,
    ColorToken::CardsDots,
];
type Draw = fn(&Shown, &Rc<Spots>, &Theme) -> Div;
const SECTIONS: [(&str, &[ColorToken], Draw); 9] = [
    ("Window", WINDOW, window),
    ("Chat", CHAT, chat),
    ("Code", SYNTAX, code),
    ("Diff", DIFF, diff),
    ("Files", FILES, files),
    ("Controls", CONTROLS, controls),
    ("Menu, toast and tooltip", OVERLAYS, overlays),
    ("Status", STATUS, status),
    ("Terminal", TERMINAL_TOKENS, terminal),
];
pub(super) const ITEMS: usize = SECTIONS.len() + 2;
const AGENTS: [(AgentKind, ColorToken); 9] = [
    (AgentKind::GoDev, ColorToken::AgentsGoDev),
    (AgentKind::TsDev, ColorToken::AgentsTsDev),
    (AgentKind::Explore, ColorToken::AgentsExplore),
    (AgentKind::Research, ColorToken::AgentsResearch),
    (AgentKind::Qa, ColorToken::AgentsQa),
    (AgentKind::Browser, ColorToken::AgentsBrowser),
    (AgentKind::PyDev, ColorToken::AgentsPyDev),
    (AgentKind::RustDev, ColorToken::AgentsRustDev),
    (AgentKind::RustDesigner, ColorToken::AgentsRustDesigner),
];
const FILE_ROWS: [(&str, GitStatus, ColorToken); 4] = [
    (
        "src/screens/theme/mod.rs",
        GitStatus::Modified,
        ColorToken::GitModified,
    ),
    (
        "src/screens/theme/preview.rs",
        GitStatus::Added,
        ColorToken::GitAdded,
    ),
    (
        "src/screens/theme/old.rs",
        GitStatus::Deleted,
        ColorToken::GitDeleted,
    ),
    (
        "notes/theme.md",
        GitStatus::Untracked,
        ColorToken::GitUntracked,
    ),
];
const STATUS_CHIPS: [(&str, ColorToken); 4] = [
    ("linked", ColorToken::StatusAccent),
    ("running", ColorToken::StatusLive),
    ("asking", ColorToken::StatusWarn),
    ("failed", ColorToken::StatusDanger),
];
type Run = (&'static str, ColorToken);
const TERMINAL: [&[Run]; 7] = [
    &[
        ("$ ", ColorToken::AnsiBrightBlack),
        ("cargo test -p desk", ColorToken::AnsiWhite),
    ],
    &[
        ("   Compiling ", ColorToken::AnsiGreen),
        ("desk v0.3.0", ColorToken::AnsiBrightWhite),
    ],
    &[
        ("warning", ColorToken::AnsiYellow),
        (": unused import ", ColorToken::AnsiBrightYellow),
        ("Rgba", ColorToken::AnsiCyan),
    ],
    &[
        ("error[E0308]", ColorToken::AnsiRed),
        (": mismatched types", ColorToken::AnsiBrightRed),
    ],
    &[
        ("  --> ", ColorToken::AnsiBlue),
        ("src/screens/theme/mod.rs:42:9", ColorToken::AnsiBrightBlue),
    ],
    &[
        ("note", ColorToken::AnsiMagenta),
        (": run with ", ColorToken::AnsiBrightMagenta),
        ("RUST_BACKTRACE=1", ColorToken::AnsiBrightCyan),
    ],
    &[
        ("test result: ", ColorToken::AnsiWhite),
        ("ok", ColorToken::AnsiBrightGreen),
        (" copied", ColorToken::AnsiBlack),
    ],
];
const SELECTED_RUN: &str = " copied";

pub(super) struct Shown {
    diff: Result<Rc<FileDiff>, SharedString>,
    code: Rc<[Block]>,
}

impl Shown {
    pub(super) fn new() -> Shown {
        let diff = FileDiff::parse("src/accent.rs", FileChange::Modified, PATCH, |_| Vec::new())
            .map(Rc::new)
            .map_err(|error| {
                SharedString::from(format!("the preview diff is not a patch: {error:?}"))
            });
        Shown {
            diff,
            code: markdown(CODE).into(),
        }
    }
}

#[derive(Default)]
pub(super) struct Spots(RefCell<Vec<(Vec<ColorToken>, Bounds<Pixels>)>>);

impl Spots {
    pub(super) fn clear(&self) {
        self.0.borrow_mut().clear();
    }

    fn of(&self, token: ColorToken) -> Vec<Bounds<Pixels>> {
        self.0
            .borrow()
            .iter()
            .filter(|(tokens, _)| tokens.contains(&token))
            .map(|(_, bounds)| *bounds)
            .collect()
    }

    pub(super) fn shows(&self, token: ColorToken, view: Bounds<Pixels>) -> bool {
        self.of(token).iter().any(|spot| spot.intersects(&view))
    }
}

pub(super) fn item_of(token: ColorToken) -> Option<usize> {
    SECTIONS
        .iter()
        .position(|(_, tokens, _)| tokens.contains(&token))
        .map(|at| at + 1)
}

pub(super) fn label(token: ColorToken) -> &'static str {
    let path = token.path();
    path.split_once('.').map_or(path, |(_, rest)| rest)
}

pub(super) fn item(at: usize, shown: &Shown, spots: &Rc<Spots>, theme: &Theme) -> AnyElement {
    desk_perf::span!("theme-preview-section");
    let body = match at.checked_sub(1).and_then(|at| SECTIONS.get(at)) {
        None if at == 0 => {
            heading("Preview", "drawn with the picked theme".into(), theme).pt(px(CARD_PAD))
        }
        None => return div().h(px(CARD_PAD)).into_any_element(),
        Some((title, _, draw)) => div()
            .flex()
            .flex_col()
            .gap_2()
            .pt(px(SECTION_GAP))
            .child(caption(*title, theme))
            .child(draw(shown, spots, theme)),
    };
    div().px(px(CARD_PAD)).child(body).into_any_element()
}

pub(super) fn rings(
    spots: &Spots,
    lit: Option<ColorToken>,
    view: Bounds<Pixels>,
    theme: &Theme,
) -> Div {
    let rings = lit.map(|token| spots.of(token)).unwrap_or_default();
    let color = theme.color(ColorToken::FocusRing);
    div().size_full().child(
        canvas(
            |_, _, _| {},
            move |_, (), window, _| {
                window.with_content_mask(
                    Some(ContentMask {
                        bounds: view,
                        fade_out: Edges::default(),
                    }),
                    |window| {
                        for ring in rings {
                            window.paint_quad(quad(
                                ring.dilate(px(LIT_SPREAD)),
                                px(ROW_RADIUS + LIT_SPREAD),
                                transparent_black(),
                                px(LIT_SPREAD),
                                color,
                                BorderStyle::Solid,
                            ));
                        }
                    },
                )
            },
        )
        .size_full(),
    )
}

fn mark<E: ParentElement + Styled>(element: E, tokens: &[ColorToken], spots: &Rc<Spots>) -> E {
    let spots = spots.clone();
    let tokens = tokens.to_vec();
    element.relative().child(
        canvas(
            move |bounds, _, _| spots.0.borrow_mut().push((tokens, bounds)),
            |_, (), _, _| {},
        )
        .absolute()
        .top_0()
        .left_0()
        .size_full(),
    )
}

fn tinted(token: ColorToken, text: impl Into<SharedString>, theme: &Theme) -> Div {
    div().text_color(theme.color(token)).child(text.into())
}

fn window(_: &Shown, spots: &Rc<Spots>, theme: &Theme) -> Div {
    let caption_button = |glyph: Icon, hovered: Option<ColorToken>| {
        div()
            .flex()
            .items_center()
            .justify_center()
            .size(px(CAPTION_BUTTON))
            .rounded(px(ROW_RADIUS))
            .when_some(hovered, |button, token| button.bg(theme.color(token)))
            .child(icon(
                glyph,
                ICON_SMALL,
                theme.color(ColorToken::TextCaption),
            ))
    };
    let bar = div()
        .flex()
        .items_center()
        .gap_3()
        .h(px(STRIP_HEIGHT))
        .px_3()
        .rounded(px(ROW_RADIUS))
        .bg(theme.color(ColorToken::SurfaceWindow))
        .child(mark(
            tinted(ColorToken::TextName, "tofu", theme).font_weight(FontWeight::SEMIBOLD),
            &[ColorToken::TextName],
            spots,
        ))
        .child(mark(
            tinted(ColorToken::TextTab, "theme", theme),
            &[ColorToken::TextTab],
            spots,
        ))
        .child(div().flex_1())
        .child(mark(
            tinted(ColorToken::TextMuted, "Ctrl K", theme).text_size(px(SMALL_TEXT)),
            &[ColorToken::TextMuted],
            spots,
        ))
        .child(mark(
            caption_button(Icon::Minimize, None),
            &[ColorToken::TextCaption],
            spots,
        ))
        .child(mark(
            caption_button(Icon::Maximize, Some(ColorToken::StateCaptionHover)),
            &[ColorToken::StateCaptionHover],
            spots,
        ))
        .child(mark(
            caption_button(Icon::Close, Some(ColorToken::StateCloseHover)),
            &[ColorToken::StateCloseHover],
            spots,
        ));
    let status = div()
        .flex()
        .items_center()
        .gap_3()
        .px_3()
        .text_size(px(SMALL_TEXT))
        .child(mark(
            tinted(ColorToken::TextStatus, "develop", theme),
            &[ColorToken::TextStatus],
            spots,
        ))
        .child(tinted(ColorToken::TextStatus, "3 cron", theme));
    div()
        .flex()
        .flex_col()
        .gap_2()
        .child(mark(bar, &[ColorToken::SurfaceWindow], spots))
        .child(status)
}

fn chat(_: &Shown, spots: &Rc<Spots>, theme: &Theme) -> Div {
    let worker = Agent {
        mark: AgentMark::Done,
        name: "rust-dev".into(),
        summary: "painted the preview in these tokens".into(),
        link: Some("DESK-608".into()),
        tone: Some(theme.color(ColorToken::AgentsRustDev)),
    };
    let finished = Worker {
        kind: AgentKind::RustDev,
        instance: 1,
        status: AgentStatus::Finished,
    };
    let people = div()
        .flex()
        .items_center()
        .gap_3()
        .child(mark(
            avatar("theme-preview-avatar", &finished, AvatarSize::Row, theme),
            &[ColorToken::AvatarRing],
            spots,
        ))
        .child(mark(
            person_avatar(
                Some(Face {
                    letter: "L".into(),
                    picture: None,
                }),
                PersonSize::Menu,
                theme,
            ),
            &[ColorToken::AccountFrom, ColorToken::AccountTo],
            spots,
        ));
    let pills = div().flex().flex_wrap().gap_1p5().children(
        AGENTS.map(|(kind, token)| mark(agent_pill(kind, kind.name(), theme), &[token], spots)),
    );
    let body = inner_card(theme)
        .relative()
        .p_3()
        .gap_3()
        .child(dots(theme))
        .child(mark(
            you(
                "now",
                "Make the accent a little warmer.",
                false,
                None,
                true,
                &[],
                theme,
            ),
            &[ColorToken::TextBase, ColorToken::TextStrong],
            spots,
        ))
        .child(agent_row("theme-preview-agent", &worker, theme))
        .child(people)
        .child(pills);
    let action = mark(
        header_action("theme-preview-action", Icon::Plus, "New chat", theme),
        &[ColorToken::TextIcon],
        spots,
    );
    mark(
        shell(
            Header::Title(None, "chat".into(), Some(action.into_any_element())),
            theme,
        )
        .child(mark(body, CARDS_INNER, spots)),
        &[ColorToken::CardsOuterFill],
        spots,
    )
}

fn code(shown: &Shown, spots: &Rc<Spots>, theme: &Theme) -> Div {
    let blamed = div()
        .flex()
        .items_center()
        .child(
            div()
                .font_family(mono(theme))
                .text_size(px(SMALL_TEXT))
                .child("let warm = mix(accent, 0.2);"),
        )
        .child(mark(
            div().child(inline_blame(
                "theme-preview-blame",
                "Luiz",
                "2 days ago",
                None,
                true,
                theme,
            )),
            &[ColorToken::TextMuted],
            spots,
        ));
    div()
        .flex()
        .flex_col()
        .gap_2()
        .child(mark(
            document("theme-preview-code", &shown.code, &[], None, theme),
            SYNTAX,
            spots,
        ))
        .child(blamed)
}

fn diff(shown: &Shown, spots: &Rc<Spots>, theme: &Theme) -> Div {
    let card = match &shown.diff {
        Ok(diff) => DiffCard::new("theme-preview-diff", diff.clone()).into_any_element(),
        Err(problem) => frame::note(problem.clone(), theme).into_any_element(),
    };
    mark(div().child(card), DIFF, spots)
}

fn files(_: &Shown, spots: &Rc<Spots>, theme: &Theme) -> Div {
    let rows = FILE_ROWS.map(|(path, state, token)| {
        mark(
            div()
                .flex()
                .items_center()
                .gap_2()
                .px_1()
                .rounded(px(ROW_RADIUS))
                .child(glyph(Glyph::File, ICON_SMALL, state.color(theme)))
                .child(git_name(path, Some(state), theme)),
            &[token],
            spots,
        )
    });
    div()
        .flex()
        .flex_col()
        .gap_1()
        .children(rows)
        .child(div().flex().pt_1().child(mark(
            mention(
                "theme-preview-mention",
                Glyph::File,
                "src/accent.rs",
                false,
                theme,
                |_, _, _| {},
            ),
            &[ColorToken::Trace, ColorToken::MentionText],
            spots,
        )))
}

fn controls(_: &Shown, spots: &Rc<Spots>, theme: &Theme) -> Div {
    let buttons = div()
        .flex()
        .flex_wrap()
        .items_center()
        .gap_2()
        .child(mark(
            button(
                "theme-preview-plain",
                "Cancel",
                None,
                ButtonKind::Plain,
                theme,
            ),
            &[ColorToken::ButtonFill],
            spots,
        ))
        .child(mark(
            button(
                "theme-preview-hovered",
                "Hovered",
                None,
                ButtonKind::Plain,
                theme,
            )
            .bg(theme.color(ColorToken::StateActive)),
            &[ColorToken::StateActive],
            spots,
        ))
        .child(mark(
            button(
                "theme-preview-primary",
                "Save",
                None,
                ButtonKind::Primary,
                theme,
            ),
            &[ColorToken::ButtonPrimary, ColorToken::ButtonPrimaryText],
            spots,
        ))
        .child(mark(
            input(
                "theme-preview-input",
                "Search tokens",
                SharedString::default(),
                false,
                theme,
            )
            .shadow(vec![ring(theme.color(ColorToken::FocusRing))]),
            &[ColorToken::FieldFill, ColorToken::FocusRing],
            spots,
        ));
    let toggles = div()
        .flex()
        .flex_wrap()
        .items_center()
        .gap_3()
        .child(mark(
            switch_bare("theme-preview-on", "Glass", true, theme),
            &[ColorToken::SwitchOn, ColorToken::SwitchThumb],
            spots,
        ))
        .child(mark(
            switch_bare("theme-preview-off", "Top light", false, theme),
            &[ColorToken::SwitchOff],
            spots,
        ))
        .child(mark(
            segmented(
                "theme-preview-mode",
                &["light", "dark"],
                1,
                theme,
                |_, _, _| {},
            ),
            &[ColorToken::SegmentedFill, ColorToken::SegmentedOn],
            spots,
        ))
        .child(flat_chip("theme-preview-pick", "12", theme));
    let tabs = mark(
        div()
            .flex()
            .items_end()
            .px_2()
            .pt_1()
            .rounded(px(ROW_RADIUS))
            .bg(theme.color(ColorToken::TabsFill))
            .child(strip_tab(
                "theme-preview-tab-on",
                "mod.rs".into(),
                true,
                theme,
            ))
            .child(strip_tab(
                "theme-preview-tab-off",
                "preview.rs".into(),
                false,
                theme,
            )),
        &[ColorToken::TabsFill],
        spots,
    );
    div()
        .flex()
        .flex_col()
        .gap_3()
        .child(buttons)
        .child(toggles)
        .child(tabs)
}

fn overlays(_: &Shown, spots: &Rc<Spots>, theme: &Theme) -> Div {
    let items = [
        MenuItem::action("Rename"),
        MenuItem::action("Duplicate"),
        MenuItem::action("Delete").danger(true),
    ];
    let hovered = mark(
        div()
            .w(px(MENU_ROW_WIDTH))
            .px_2()
            .py_1()
            .rounded(px(ROW_RADIUS))
            .bg(theme.color(ColorToken::StateHover))
            .child("Hovered row"),
        &[ColorToken::StateHover],
        spots,
    );
    let tip = mark(
        overlay_surface(theme)
            .px_2()
            .py_1()
            .text_size(px(SMALL_TEXT))
            .child("Open the colour picker"),
        &[ColorToken::Shadow],
        spots,
    );
    div()
        .flex()
        .flex_wrap()
        .items_start()
        .gap_3()
        .child(mark(
            div().child(menu("theme-preview-menu", &items, theme, |_, _, _| {}).selected(Some(0))),
            &[ColorToken::StatusDanger],
            spots,
        ))
        .child(div().flex().flex_col().gap_3().child(hovered).child(tip))
        .child(mark(
            div().child(toast("Theme saved".into(), TELL_BADGE, theme, |_, _, _| {})),
            &[ColorToken::ToastFill],
            spots,
        ))
}

fn status(_: &Shown, spots: &Rc<Spots>, theme: &Theme) -> Div {
    div()
        .flex()
        .flex_wrap()
        .gap_2()
        .children(STATUS_CHIPS.map(|(label, token)| {
            mark(
                div().child(dot_chip(label, rgb_to_hsla(theme.color(token)))),
                &[token],
                spots,
            )
        }))
}

fn terminal(_: &Shown, spots: &Rc<Spots>, theme: &Theme) -> Div {
    let lines = TERMINAL.map(|runs| {
        let mut text = String::new();
        let mut highlights: Vec<(Range<usize>, HighlightStyle)> = Vec::new();
        for (run, token) in runs {
            let start = text.len();
            text.push_str(run);
            highlights.push((
                start..text.len(),
                HighlightStyle {
                    color: Some(rgb_to_hsla(theme.color(*token))),
                    background_color: (*run == SELECTED_RUN)
                        .then(|| rgb_to_hsla(theme.color(ColorToken::Selection))),
                    ..HighlightStyle::default()
                },
            ));
        }
        let tokens: Vec<ColorToken> = runs
            .iter()
            .map(|(_, token)| *token)
            .chain(
                runs.iter()
                    .any(|(run, _)| *run == SELECTED_RUN)
                    .then_some(ColorToken::Selection),
            )
            .collect();
        mark(
            div()
                .rounded(px(ROW_RADIUS))
                .whitespace_nowrap()
                .child(StyledText::new(text).with_highlights(highlights)),
            &tokens,
            spots,
        )
    });
    div()
        .flex()
        .flex_col()
        .gap_0p5()
        .p_3()
        .rounded(px(ROW_RADIUS))
        .bg(theme.color(ColorToken::SurfaceWindow))
        .font_family(mono(theme))
        .text_size(px(SMALL_TEXT))
        .children(lines)
}
