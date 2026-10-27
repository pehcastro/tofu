use std::borrow::Cow;

use gpui::{
    AssetData, AssetEntry, ImageSource, IntoElement, ParentElement, Pixels, Resource, Rgba,
    SharedString, Styled, div, img, rgb, svg,
};

use crate::components::paint::{ink, solid};
use crate::components::size::T1;
use crate::theme::{ColorToken, Mode, Theme};

const MIN_CONTRAST: f32 = 3.0;
const RUNGS: [u16; 7] = [12, 16, 20, 24, 32, 48, 64];
const LARGEST_RUNG: u16 = 64;

#[derive(Clone, Copy, Debug, PartialEq)]
pub enum Paint {
    Tinted { brand: Option<Rgba> },
    Own,
}

const MONO: Paint = Paint::Tinted { brand: None };
const OWN: Paint = Paint::Own;

fn brand(hex: u32) -> Paint {
    Paint::Tinted {
        brand: Some(rgb(hex)),
    }
}

macro_rules! brands {
    ($($variant:ident => $slug:literal, $label:literal, $paint:expr;)+) => {
        #[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
        pub enum Brand {
            $($variant),+
        }

        impl Brand {
            pub const ALL: [Brand; [$($slug),+].len()] = [$(Brand::$variant),+];

            pub fn slug(self) -> &'static str {
                match self {
                    $(Brand::$variant => $slug),+
                }
            }

            pub fn label(self) -> &'static str {
                match self {
                    $(Brand::$variant => $label),+
                }
            }

            pub fn paint(self) -> Paint {
                match self {
                    $(Brand::$variant => $paint),+
                }
            }

            fn path(self) -> &'static str {
                match self {
                    $(Brand::$variant => concat!("logos/", $slug, ".svg")),+
                }
            }

            fn svg(self) -> &'static str {
                match self {
                    $(Brand::$variant => include_str!(concat!("../../../../assets/logos/", $slug, ".svg"))),+
                }
            }

            fn raster(self, rung: u16) -> &'static str {
                match rung {
                    12 => match self { $(Brand::$variant => concat!("logos/12/", $slug, ".svg")),+ },
                    16 => match self { $(Brand::$variant => concat!("logos/16/", $slug, ".svg")),+ },
                    20 => match self { $(Brand::$variant => concat!("logos/20/", $slug, ".svg")),+ },
                    24 => match self { $(Brand::$variant => concat!("logos/24/", $slug, ".svg")),+ },
                    32 => match self { $(Brand::$variant => concat!("logos/32/", $slug, ".svg")),+ },
                    48 => match self { $(Brand::$variant => concat!("logos/48/", $slug, ".svg")),+ },
                    _ => match self { $(Brand::$variant => concat!("logos/64/", $slug, ".svg")),+ },
                }
            }
        }
    };
}

brands! {
    Anthropic => "anthropic", "Anthropic", MONO;
    Claude => "claude", "Claude", brand(0xd97757);
    ClaudeCode => "claude-code", "Claude Code", brand(0xd97757);
    OpenAi => "openai", "OpenAI", MONO;
    Codex => "codex", "Codex", MONO;
    OpenCode => "opencode", "OpenCode", MONO;
    OpenRouter => "openrouter", "OpenRouter", MONO;
    TypeSafe => "typesafe", "TypeSafe", MONO;
    Meta => "meta", "Meta", OWN;
    Gemini => "gemini", "Gemini", OWN;
    GeminiCli => "gemini-cli", "Gemini CLI", OWN;
    DeepMind => "deepmind", "DeepMind", brand(0x4285f4);
    Antigravity => "antigravity", "Antigravity", OWN;
    FirebaseStudio => "firebase-studio", "Firebase Studio", OWN;
    DeepSeek => "deepseek", "DeepSeek", brand(0x4d6bfe);
    Grok => "grok", "Grok", MONO;
    Xai => "xai", "xAI", MONO;
    Mistral => "mistral", "Mistral", OWN;
    Qwen => "qwen", "Qwen", MONO;
    Moonshot => "moonshot", "Moonshot AI", MONO;
    Perplexity => "perplexity", "Perplexity", brand(0x1fb8cd);
    HuggingFace => "hugging-face", "Hugging Face", OWN;
    Ollama => "ollama", "Ollama", MONO;
    LmStudio => "lm-studio", "LM Studio", MONO;
    LlamaCpp => "llama-cpp", "llama.cpp", brand(0xff8236);
    LiteLlm => "litellm", "LiteLLM", OWN;
    Exa => "exa", "Exa", brand(0x1f40ed);
    Fal => "fal", "fal", brand(0xec0648);
    Firecrawl => "firecrawl", "Firecrawl", MONO;
    Microsoft365Copilot => "microsoft-365-copilot", "Microsoft 365 Copilot", OWN;
    MicrosoftCopilot => "microsoft-copilot", "Microsoft Copilot", OWN;
    GitHub => "github", "GitHub", MONO;
    Linear => "linear", "Linear", brand(0x5e6ad2);
    Jira => "jira", "Jira", brand(0x0052cc);
    Notion => "notion", "Notion", MONO;
    Vercel => "vercel", "Vercel", MONO;
    Cloudflare => "cloudflare", "Cloudflare", OWN;
    GoogleCloud => "google-cloud", "Google Cloud", OWN;
    Docker => "docker", "Docker", brand(0x008fe2);
    NodeJs => "nodejs", "Node.js", brand(0x539e43);
    Bun => "bun", "Bun", OWN;
    Npm => "npm", "npm", OWN;
    Pnpm => "pnpm", "pnpm", OWN;
    Apple => "apple", "Apple", MONO;
    Android => "android", "Android", OWN;
    Windows => "windows", "Windows", brand(0x00adef);
    Microsoft => "microsoft", "Microsoft", OWN;
    Google => "google", "Google", OWN;
    GoogleMono => "google-mono", "Google", MONO;
    Gmail => "gmail", "Gmail", OWN;
    GoogleCalendar => "google-calendar", "Google Calendar", OWN;
    GoogleDrive => "google-drive", "Google Drive", OWN;
    YouTube => "youtube", "YouTube", OWN;
    Discord => "discord", "Discord", brand(0x5865f2);
    LinkedIn => "linkedin", "LinkedIn", brand(0x0a66c2);
    Chrome => "chrome", "Chrome", OWN;
    Chromium => "chromium", "Chromium", OWN;
    Firefox => "firefox", "Firefox", OWN;
    Safari => "safari", "Safari", OWN;
    Edge => "edge", "Edge", OWN;
    Brave => "brave", "Brave", OWN;
    Opera => "opera", "Opera", OWN;
    DuckDuckGo => "duckduckgo", "DuckDuckGo", OWN;
    Zen => "zen", "Zen", brand(0xf76f53);
}

impl Brand {
    pub fn for_source(source: &str) -> Option<Brand> {
        match source {
            "claude-sub" => Some(Brand::Claude),
            "codex-sub" => Some(Brand::Codex),
            "opencode-sub" => Some(Brand::OpenCode),
            "anthropic" => Some(Brand::Anthropic),
            "openai" => Some(Brand::OpenAi),
            "openrouter" => Some(Brand::OpenRouter),
            "typesafe" => Some(Brand::TypeSafe),
            "meta" => Some(Brand::Meta),
            _ => None,
        }
    }

    pub fn for_name(name: &str) -> Option<Brand> {
        Brand::ALL.into_iter().find(|brand| {
            name.eq_ignore_ascii_case(brand.slug()) || name.eq_ignore_ascii_case(brand.label())
        })
    }

    pub fn entries() -> Vec<(&'static str, AssetEntry)> {
        let mut entries = Vec::new();
        for brand in Brand::ALL {
            let svg = brand.svg();
            match brand.paint() {
                Paint::Tinted { .. } => entries.push((
                    brand.path(),
                    AssetEntry::PreLoaded(Cow::Borrowed(svg.as_bytes())),
                )),
                Paint::Own => entries.extend(RUNGS.map(|rung| {
                    (
                        brand.raster(rung),
                        AssetEntry::from(move || Some(sized(svg, rung))),
                    )
                })),
            }
        }
        entries
    }
}

fn sized(svg: &str, rung: u16) -> AssetData {
    let root = format!("<svg width=\"{rung}\" height=\"{rung}\" ");
    Cow::Owned(svg.replacen("<svg ", &root, 1).into_bytes())
}

pub fn logo(brand: Brand, size: Pixels, theme: &Theme) -> impl IntoElement {
    let frame = div().flex_none().size(size);
    match brand.paint() {
        Paint::Own => {
            let rung = RUNGS
                .into_iter()
                .find(|rung| f32::from(*rung) >= f32::from(size))
                .unwrap_or(LARGEST_RUNG);
            let source = Resource::Embedded(SharedString::new_static(brand.raster(rung)));
            frame.child(img(ImageSource::Resource(source)).size_full())
        }
        Paint::Tinted { brand: color } => frame.child(
            svg()
                .path(SharedString::new_static(brand.path()))
                .size_full()
                .text_color(tinted(color, theme)),
        ),
    }
}

fn tinted(color: Option<Rgba>, theme: &Theme) -> Rgba {
    match color {
        Some(color) if contrast(color, surface(theme)) >= MIN_CONTRAST => color,
        _ => ink(theme, T1),
    }
}

fn surface(theme: &Theme) -> Rgba {
    let backdrop = match theme.mode() {
        Mode::Dark => rgb(0x000000),
        Mode::Light => rgb(0xffffff),
    };
    [
        ColorToken::SurfaceWindow,
        ColorToken::CardsOuterFill,
        ColorToken::CardsInnerFill,
    ]
    .into_iter()
    .fold(backdrop, |under, token| solid(theme.color(token), under))
}

fn contrast(one: Rgba, other: Rgba) -> f32 {
    let (one, other) = (luminance(one), luminance(other));
    (one.max(other) + 0.05) / (one.min(other) + 0.05)
}

fn luminance(color: Rgba) -> f32 {
    let linear = |channel: f32| {
        if channel <= 0.04045 {
            channel / 12.92
        } else {
            ((channel + 0.055) / 1.055).powf(2.4)
        }
    };
    0.2126 * linear(color.red) + 0.7152 * linear(color.green) + 0.0722 * linear(color.blue)
}
