use std::collections::BTreeMap;
use std::error::Error;
use std::fmt::Write as _;
use std::path::Path;

use serde_json::{Map, Value};

type Fallible<T> = Result<T, Box<dyn Error>>;
type Enums = BTreeMap<(String, Vec<String>), String>;

const SCHEMA: &str = "schema/tofu.host.json";
const KEYWORDS: [&str; 49] = [
    "abstract", "as", "async", "await", "become", "box", "break", "const", "continue", "do", "dyn",
    "else", "enum", "extern", "false", "final", "fn", "for", "gen", "if", "impl", "in", "let",
    "loop", "macro", "match", "mod", "move", "mut", "override", "priv", "pub", "ref", "return",
    "self", "static", "struct", "trait", "true", "try", "type", "typeof", "unsafe", "unsized",
    "use", "virtual", "where", "while", "yield",
];

struct FieldType {
    rust: String,
    emptied_by: Option<&'static str>,
    defaultable: bool,
}

fn main() -> Fallible<()> {
    println!("cargo:rerun-if-changed={SCHEMA}");
    let schema: Value = serde_json::from_str(&std::fs::read_to_string(SCHEMA)?)?;
    let code = generate(&schema)?;
    std::fs::write(
        Path::new(&std::env::var("OUT_DIR")?).join("protocol.rs"),
        code,
    )?;
    Ok(())
}

fn generate(schema: &Value) -> Fallible<String> {
    let structs: Vec<(&String, &Map<String, Value>)> = object(schema, "$defs")?
        .iter()
        .filter(|(_, def)| def.get("oneOf").is_none())
        .map(|(name, def)| {
            Ok((
                name,
                def.as_object().ok_or(format!("{name} is not an object"))?,
            ))
        })
        .collect::<Fallible<_>>()?;
    let enums = enumerations(&structs)?;
    let mut out = String::new();
    let id = schema["$id"].as_str().ok_or("the schema has no $id")?;
    writeln!(out, "pub const PROTOCOL: &str = {id:?};")?;
    let mut packages: BTreeMap<Option<&str>, String> = BTreeMap::new();
    for (name, def) in &structs {
        let (package, local) = match name.split_once('.') {
            Some((package, local)) => (Some(package), local),
            None => (None, name.as_str()),
        };
        write_struct(packages.entry(package).or_default(), local, def, &enums)?;
    }
    for (package, code) in &packages {
        match package {
            Some(package) => writeln!(out, "pub mod {package} {{\nuse super::*;\n{code}}}")?,
            None => out.push_str(code),
        }
    }
    for ((_, values), name) in &enums {
        write_enum(&mut out, name, values)?;
    }
    write_methods(&mut out, "Notification", object(schema, "x-notifications")?)?;
    write_methods(
        &mut out,
        "ServerRequest",
        object(schema, "x-serverRequests")?,
    )?;
    writeln!(out, "pub mod request {{")?;
    for (method, entry) in object(schema, "x-requests")? {
        writeln!(
            out,
            "pub struct {}; impl super::Request for {0} {{ const METHOD: &'static str = {method:?}; type Params = super::{}; type Result = super::{}; }}",
            pascal(method),
            reference(&entry["params"])?,
            reference(&entry["result"])?,
        )?;
    }
    writeln!(out, "}}")?;
    Ok(out)
}

fn object<'a>(value: &'a Value, key: &str) -> Fallible<&'a Map<String, Value>> {
    Ok(value
        .get(key)
        .and_then(Value::as_object)
        .ok_or(format!("the schema has no {key}"))?)
}

fn properties(def: &Map<String, Value>) -> Fallible<&Map<String, Value>> {
    Ok(def
        .get("properties")
        .and_then(Value::as_object)
        .ok_or("a definition has no properties")?)
}

fn reference(value: &Value) -> Fallible<String> {
    Ok(value["$ref"]
        .as_str()
        .and_then(|path| path.strip_prefix("#/$defs/"))
        .ok_or(format!("{value} is not a reference to a definition"))?
        .replacen('.', "::", 1))
}

fn enumerations(structs: &[(&String, &Map<String, Value>)]) -> Fallible<Enums> {
    let mut owners: BTreeMap<(String, Vec<String>), Vec<String>> = BTreeMap::new();
    for (name, def) in structs {
        for (field, property) in properties(def)? {
            if let Some(values) = property.get("enum") {
                let values = serde_json::from_value::<Vec<String>>(values.clone())?;
                owners
                    .entry((field.clone(), values))
                    .or_default()
                    .push(pascal(name));
            }
        }
    }
    let mut enums = Enums::new();
    for (key, structs) in owners {
        let name = common_words(&structs) + &pascal(&key.0);
        if enums.values().any(|taken| *taken == name) {
            return Err(format!("two enums of different values are both named {name}").into());
        }
        enums.insert(key, name);
    }
    Ok(enums)
}

fn common_words(structs: &[String]) -> String {
    let split: Vec<Vec<String>> = structs.iter().map(|name| words(name)).collect();
    let first = split.first().cloned().unwrap_or_default();
    first
        .iter()
        .enumerate()
        .take_while(|(at, word)| split.iter().all(|other| other.get(*at) == Some(*word)))
        .map(|(_, word)| word.as_str())
        .collect()
}

fn words(name: &str) -> Vec<String> {
    let mut out: Vec<String> = Vec::new();
    for c in name.chars() {
        match out.last_mut() {
            Some(word) if !c.is_uppercase() => word.push(c),
            _ => out.push(c.to_string()),
        }
    }
    out
}

fn write_struct(
    out: &mut String,
    name: &str,
    def: &Map<String, Value>,
    enums: &Enums,
) -> Fallible<()> {
    let required: Vec<String> =
        serde_json::from_value(def.get("required").cloned().unwrap_or_default())?;
    let mut fields = String::new();
    let mut defaultable = true;
    for (field, property) in properties(def)? {
        let shape = field_type(field, property, enums)?;
        let needed = required.contains(field);
        let rename = format!("rename = {field:?}");
        let (attributes, rust) = match (needed, shape.emptied_by) {
            (true, Some(_)) => (
                format!("{rename}, deserialize_with = \"null_as_empty\""),
                shape.rust,
            ),
            (true, None) => (rename, shape.rust),
            (false, Some(is_empty)) => (
                format!(
                    "{rename}, default, deserialize_with = \"null_as_empty\", skip_serializing_if = {is_empty:?}"
                ),
                shape.rust,
            ),
            (false, None) => (
                format!("{rename}, default, skip_serializing_if = \"Option::is_none\""),
                format!("Option<{}>", shape.rust),
            ),
        };
        defaultable &= shape.defaultable || !needed;
        writeln!(
            fields,
            "    #[serde({attributes})]\n    pub {}: {rust},",
            snake(field)
        )?;
    }
    let default = if defaultable { ", Default" } else { "" };
    writeln!(
        out,
        "#[derive(Clone, Debug, PartialEq, Serialize, Deserialize{default})]"
    )?;
    writeln!(out, "pub struct {name} {{\n{fields}}}")?;
    Ok(())
}

fn field_type(field: &str, property: &Value, enums: &Enums) -> Fallible<FieldType> {
    let scalar = |rust: &str| FieldType {
        rust: rust.to_owned(),
        emptied_by: None,
        defaultable: true,
    };
    let named = |rust: String| FieldType {
        rust,
        emptied_by: None,
        defaultable: false,
    };
    if let Some(values) = property.get("enum") {
        let key = (
            field.to_owned(),
            serde_json::from_value::<Vec<String>>(values.clone())?,
        );
        return Ok(named(
            enums
                .get(&key)
                .cloned()
                .ok_or(format!("{field} has no enum"))?,
        ));
    }
    if property.get("$ref").is_some() {
        return Ok(named(reference(property)?));
    }
    let kinds: Vec<&str> = match property.get("type") {
        None if property.as_object().is_some_and(Map::is_empty) => {
            return Ok(scalar("serde_json::Value"));
        }
        Some(Value::String(kind)) => vec![kind.as_str()],
        Some(Value::Array(kinds)) => kinds.iter().filter_map(Value::as_str).collect(),
        _ => {
            return Err(
                format!("{field} has a type this generator does not read: {property}").into(),
            );
        }
    };
    match kinds.as_slice() {
        ["string"] => Ok(scalar("String")),
        ["integer"] => Ok(scalar("i64")),
        ["number"] => Ok(scalar("f64")),
        ["boolean"] => Ok(scalar("bool")),
        ["array"] | ["array", "null"] => {
            let item = field_type(field, &property["items"], enums)?;
            Ok(FieldType {
                rust: format!("Vec<{}>", item.rust),
                emptied_by: Some("Vec::is_empty"),
                defaultable: true,
            })
        }
        ["object"] | ["object", "null"] => {
            let value = field_type(field, &property["additionalProperties"], enums)?;
            Ok(FieldType {
                rust: format!("std::collections::BTreeMap<String, {}>", value.rust),
                emptied_by: Some("std::collections::BTreeMap::is_empty"),
                defaultable: true,
            })
        }
        _ => Err(format!("{field} has a type this generator does not read: {property}").into()),
    }
}

fn write_enum(out: &mut String, name: &str, values: &[String]) -> Fallible<()> {
    let variants: Vec<(String, &String)> =
        values.iter().map(|value| (pascal(value), value)).collect();
    if variants.iter().any(|(variant, _)| variant == "Unknown") {
        return Err(
            format!("{name} has a value named unknown, which Unknown(raw) already is").into(),
        );
    }
    let list: String = variants
        .iter()
        .map(|(variant, _)| format!("{variant}, "))
        .collect();
    let from: String = variants
        .iter()
        .map(|(variant, value)| format!("{value:?} => Self::{variant}, "))
        .collect();
    let into: String = variants
        .iter()
        .map(|(variant, value)| format!("{name}::{variant} => {value:?}.to_owned(), "))
        .collect();
    writeln!(
        out,
        "#[derive(Clone, Debug, PartialEq, Eq, Hash, Serialize, Deserialize)]\n#[serde(from = \"String\", into = \"String\")]\npub enum {name} {{ {list}Unknown(String) }}\n\
         impl From<String> for {name} {{ fn from(raw: String) -> Self {{ match raw.as_str() {{ {from}_ => Self::Unknown(raw) }} }} }}\n\
         impl From<{name}> for String {{ fn from(value: {name}) -> Self {{ match value {{ {into}{name}::Unknown(raw) => raw }} }} }}"
    )?;
    Ok(())
}

fn write_methods(out: &mut String, name: &str, methods: &Map<String, Value>) -> Fallible<()> {
    let mut variants = String::new();
    let mut arms = String::new();
    for (method, entry) in methods {
        let variant = pascal(method);
        let params = entry.get("params").unwrap_or(entry);
        writeln!(variants, "    {variant}(Box<{}>),", reference(params)?)?;
        writeln!(
            arms,
            "            {method:?} => Self::{variant}(serde_json::from_value(params)?),"
        )?;
    }
    writeln!(
        out,
        "#[derive(Clone, Debug, PartialEq)]\npub enum {name} {{\n{variants}    Unknown {{ method: String, params: serde_json::Value }},\n}}\n\
         impl {name} {{\n    pub fn parse(method: &str, params: serde_json::Value) -> Result<Self, serde_json::Error> {{\n        Ok(match method {{\n{arms}            _ => Self::Unknown {{ method: method.to_owned(), params }},\n        }})\n    }}\n}}"
    )?;
    Ok(())
}

fn pascal(name: &str) -> String {
    name.split(|c: char| !c.is_ascii_alphanumeric())
        .flat_map(|piece| {
            let mut chars = piece.chars();
            chars
                .next()
                .map(|first| first.to_ascii_uppercase())
                .into_iter()
                .chain(chars)
        })
        .collect()
}

fn snake(name: &str) -> String {
    let mut out = String::new();
    for c in name.chars() {
        if c.is_ascii_uppercase() {
            out.push('_');
        }
        out.push(c.to_ascii_lowercase());
    }
    if KEYWORDS.contains(&out.as_str()) {
        format!("r#{out}")
    } else {
        out
    }
}
