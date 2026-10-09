macro_rules! kinds {
    ($($variant:ident => $name:literal),+ $(,)?) => {
        #[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
        pub enum Kind {
            $($variant),+
        }

        impl Kind {
            pub const ALL: [Kind; [$($name),+].len()] = [$(Kind::$variant),+];

            pub fn name(self) -> &'static str {
                match self {
                    $(Kind::$variant => $name),+
                }
            }
        }
    };
}

kinds!(
    Attribute => "attribute",
    Boolean => "boolean",
    Comment => "comment",
    CommentDoc => "comment.doc",
    Constant => "constant",
    Constructor => "constructor",
    Embedded => "embedded",
    Emphasis => "emphasis",
    EmphasisStrong => "emphasis.strong",
    Enum => "enum",
    Function => "function",
    Keyword => "keyword",
    Label => "label",
    LinkText => "link_text",
    LinkUri => "link_uri",
    Namespace => "namespace",
    Number => "number",
    Operator => "operator",
    Preproc => "preproc",
    Property => "property",
    Punctuation => "punctuation",
    PunctuationBracket => "punctuation.bracket",
    PunctuationDelimiter => "punctuation.delimiter",
    PunctuationListMarker => "punctuation.list_marker",
    PunctuationMarkup => "punctuation.markup",
    PunctuationSpecial => "punctuation.special",
    Selector => "selector",
    SelectorPseudo => "selector.pseudo",
    String => "string",
    StringEscape => "string.escape",
    StringRegex => "string.regex",
    StringSpecial => "string.special",
    StringSpecialSymbol => "string.special.symbol",
    Tag => "tag",
    TextLiteral => "text.literal",
    Title => "title",
    Type => "type",
    Variable => "variable",
    VariableParameter => "variable.parameter",
    VariableSpecial => "variable.special",
    Variant => "variant",
);

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(super) enum Capture {
    Colour(Kind),
    Clear,
    Unknown,
    Internal,
}

impl Kind {
    fn named(name: &str) -> Option<Self> {
        let alias = match name {
            "escape" => Some(Self::StringEscape),
            "float" => Some(Self::Number),
            "method" => Some(Self::Function),
            "module" => Some(Self::Namespace),
            "lifetime" => Some(Self::Label),
            "variable.builtin" => Some(Self::VariableSpecial),
            "comment.documentation" => Some(Self::CommentDoc),
            "text.title" | "markup.heading" => Some(Self::Title),
            "text.uri" | "markup.link.url" => Some(Self::LinkUri),
            "text.reference" | "markup.link.label" | "markup.link.text" => Some(Self::LinkText),
            "text.emphasis" | "markup.italic" => Some(Self::Emphasis),
            "text.strong" | "markup.strong" => Some(Self::EmphasisStrong),
            "markup.raw" => Some(Self::TextLiteral),
            "markup.list" => Some(Self::PunctuationListMarker),
            _ => None,
        };
        alias.or_else(|| Self::ALL.into_iter().find(|kind| kind.name() == name))
    }

    pub(super) fn capture(name: &str) -> Capture {
        if name == "none" {
            return Capture::Clear;
        }
        if name.starts_with('_') || name.starts_with("injection.") {
            return Capture::Internal;
        }
        let mut prefix = name;
        loop {
            if let Some(kind) = Self::named(prefix) {
                return Capture::Colour(kind);
            }
            match prefix.rsplit_once('.') {
                Some((head, _)) => prefix = head,
                None => return Capture::Unknown,
            }
        }
    }
}
