use std::collections::BTreeSet;

use wasmparser::component_types::{ComponentAnyTypeId, ComponentEntityType};
use wasmparser::{ComponentTypeRef, KnownCustom, Parser, Payload, Validator, WasmFeatures};

use crate::{Capability, Error, Manifest, capability_of};

pub const MIN_KEY_LEN: usize = 20;
pub const KEY_ENTROPY_BITS: f64 = 4.0;
pub const KEY_PREFIXES: [&str; 5] = ["sk-", "ghp_", "xox", "AKIA", "AIza"];
const SCHEMES: [&str; 2] = ["http://", "https://"];
const SHOWN_PREFIX: usize = 4;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Import {
    pub module: String,
    pub name: String,
    pub capability: Option<Capability>,
    pub declared: bool,
}

#[derive(Debug, Clone, PartialEq)]
pub struct KeyLike {
    pub offset: u64,
    pub prefix: String,
    pub entropy: f64,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct ScanReport {
    pub imports: Vec<Import>,
    pub hosts: Vec<String>,
    pub urls: Vec<String>,
    pub key_like: Vec<KeyLike>,
    pub size: u64,
    pub producer: Option<String>,
    pub exports: Vec<String>,
}

pub fn scan(wasm: &[u8], manifest: &Manifest) -> Result<ScanReport, Error> {
    let types = Validator::new_with_features(WasmFeatures::all())
        .validate_all(wasm)
        .map_err(Error::Scan)?;
    let import = |module: &str, name: &str| {
        let capability = capability_of(manifest.api, module, name);
        Import {
            module: module.to_owned(),
            name: name.to_owned(),
            capability,
            declared: capability.is_some_and(|c| manifest.capabilities.contains_key(&c)),
        }
    };
    let mut report = ScanReport {
        size: wasm.len() as u64,
        ..ScanReport::default()
    };
    let mut urls = BTreeSet::new();
    let mut languages = BTreeSet::new();
    let mut tools = BTreeSet::new();
    let mut depth = 0usize;
    for payload in Parser::new(0).parse_all(wasm) {
        let top = depth == 1;
        match payload.map_err(Error::Scan)? {
            Payload::Version { .. } => depth += 1,
            Payload::End(_) => depth = depth.saturating_sub(1),
            Payload::ImportSection(reader) if top => {
                for found in reader.into_imports() {
                    let found = found.map_err(Error::Scan)?;
                    report.imports.push(import(found.module, found.name));
                }
            }
            Payload::ComponentImportSection(reader) if top => {
                for found in reader {
                    let found = found.map_err(Error::Scan)?;
                    let name = found.name.name;
                    match found.ty {
                        ComponentTypeRef::Type(_) => {}
                        ComponentTypeRef::Func(_) => report.imports.push(import("", name)),
                        ComponentTypeRef::Instance(index) => {
                            let ComponentAnyTypeId::Instance(id) =
                                types.as_ref().component_any_type_at(index)
                            else {
                                report.imports.push(import(name, ""));
                                continue;
                            };
                            for (func, item) in &types[id].exports {
                                if matches!(item.ty, ComponentEntityType::Func(_)) {
                                    report.imports.push(import(name, func));
                                }
                            }
                        }
                        _ => report.imports.push(import(name, "")),
                    }
                }
            }
            Payload::ExportSection(reader) if top => {
                for found in reader {
                    report
                        .exports
                        .push(found.map_err(Error::Scan)?.name.to_owned());
                }
            }
            Payload::ComponentExportSection(reader) if top => {
                for found in reader {
                    report
                        .exports
                        .push(found.map_err(Error::Scan)?.name.name.to_owned());
                }
            }
            Payload::DataSection(reader) => {
                for data in reader {
                    let data = data.map_err(Error::Scan)?;
                    let start = data.range.end.saturating_sub(data.data.len());
                    links(data.data, &mut urls);
                    keys(data.data, start, &mut report.key_like);
                }
            }
            Payload::CustomSection(custom) => {
                links(custom.data(), &mut urls);
                if let KnownCustom::Producers(reader) = custom.as_known() {
                    for field in reader {
                        let field = field.map_err(Error::Scan)?;
                        let into = match field.name {
                            "language" => &mut languages,
                            "processed-by" => &mut tools,
                            _ => continue,
                        };
                        for value in field.values {
                            let value = value.map_err(Error::Scan)?;
                            into.insert(
                                format!("{} {}", value.name, value.version)
                                    .trim()
                                    .to_owned(),
                            );
                        }
                    }
                }
            }
            _ => {}
        }
    }
    let hosts: BTreeSet<String> = urls.iter().filter_map(|u| host(u)).collect();
    report.hosts = hosts.into_iter().collect();
    report.urls = urls.into_iter().collect();
    report.producer = [languages, tools]
        .into_iter()
        .find(|named| !named.is_empty())
        .map(|named| named.into_iter().collect::<Vec<_>>().join(", "));
    Ok(report)
}

fn is_url_byte(b: u8) -> bool {
    b.is_ascii_graphic()
        && !matches!(
            b,
            b'"' | b'\'' | b'<' | b'>' | b'`' | b'\\' | b'{' | b'}' | b'|' | b'^'
        )
}

fn links(bytes: &[u8], into: &mut BTreeSet<String>) {
    for (at, _) in bytes.iter().enumerate().filter(|&(_, &b)| b == b'h') {
        let rest = bytes.get(at..).unwrap_or_default();
        if !SCHEMES.iter().any(|s| rest.starts_with(s.as_bytes())) {
            continue;
        }
        let end = rest
            .iter()
            .position(|&b| !is_url_byte(b))
            .unwrap_or(rest.len());
        let url = String::from_utf8_lossy(rest.get(..end).unwrap_or_default()).into_owned();
        if host(&url).is_some() {
            into.insert(url);
        }
    }
}

fn host(url: &str) -> Option<String> {
    let (_, rest) = url.split_once("://")?;
    let end = rest.find(['/', ':', '?', '#']).unwrap_or(rest.len());
    let host = rest.get(..end)?;
    (!host.is_empty()).then(|| host.to_ascii_lowercase())
}

fn is_key_byte(b: u8) -> bool {
    b.is_ascii_alphanumeric() || matches!(b, b'-' | b'_')
}

fn is_mixed(token: &[u8]) -> bool {
    [
        u8::is_ascii_uppercase,
        u8::is_ascii_lowercase,
        u8::is_ascii_digit,
    ]
    .iter()
    .all(|class| token.iter().any(class))
}

fn keys(bytes: &[u8], base: usize, into: &mut Vec<KeyLike>) {
    let mut start = None;
    for (at, byte) in bytes.iter().map(Some).chain([None]).enumerate() {
        match (start, byte.is_some_and(|&b| is_key_byte(b))) {
            (None, true) => start = Some(at),
            (Some(from), false) => {
                start = None;
                let token = bytes.get(from..at).unwrap_or_default();
                into.extend(key_like(token, base.saturating_add(from)));
            }
            _ => {}
        }
    }
}

fn key_like(token: &[u8], offset: usize) -> Option<KeyLike> {
    if token.len() < MIN_KEY_LEN {
        return None;
    }
    let entropy = entropy(token);
    let known = KEY_PREFIXES.iter().any(|p| token.starts_with(p.as_bytes()));
    (known || (entropy > KEY_ENTROPY_BITS && is_mixed(token))).then(|| KeyLike {
        offset: offset as u64,
        prefix: String::from_utf8_lossy(token.get(..SHOWN_PREFIX).unwrap_or_default()).into_owned(),
        entropy,
    })
}

fn entropy(token: &[u8]) -> f64 {
    let mut counts = [0u32; 256];
    for &b in token {
        counts[usize::from(b)] += 1;
    }
    let len = token.len() as f64;
    counts
        .iter()
        .filter(|&&n| n > 0)
        .map(|&n| {
            let p = f64::from(n) / len;
            -p * p.log2()
        })
        .sum()
}
