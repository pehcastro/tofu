use std::error::Error;
use std::fs;
use std::path::{Path, PathBuf};
use std::time::Instant;

use desk_plugin::{Import, MANIFEST_FILE, Manifest, ScanReport, scan};
use wasm_encoder::{
    ConstExpr, DataSection, EntityType, ExportKind, ExportSection, ImportSection, MemorySection,
    MemoryType, Module, TypeSection,
};
use wit_component::ComponentEncoder;

const RUNS: usize = 10;
const PAD_BYTES: usize = 5 << 20;
const PAGE: usize = 1 << 16;
const NOTIFY: (&str, &str) = ("tofu:desk/host@0.1.0", "notify");
const SOCKET: (&str, &str) = ("wasi:sockets/tcp@0.2.0", "connect");
const EVIL: &[&[u8]] = &[
    b"https://evil.example/x",
    b"http://evil.example:8080/upload?k=1",
];
const KEY: &[&[u8]] = &[b"sk-Zq8vT3nW0xLk5RmB7yHc2JdF9sGp4Ae6Uo1Ni"];
const EVIL_TOML: &str = "id = \"luiz.evil\"\nname = \"Evil\"\nicon = \"warning\"\nversion = \"0.1.0\"\napi = \"desk@0.1\"\ntofu = \">=0.6\"\n";
const KEY_TOML: &str = "id = \"luiz.key\"\nname = \"Key\"\nicon = \"warning\"\nversion = \"0.1.0\"\napi = \"desk@0.1\"\ntofu = \">=0.6\"\n\n[capabilities]\nnotify = true\n";

fn guest(imports: &[(&str, &str)], strings: &[&[u8]], pad: &[u8]) -> Vec<u8> {
    let mut data: Vec<u8> = strings.join(&0u8).into_iter().chain([0]).collect();
    data.extend_from_slice(pad);
    let mut types = TypeSection::new();
    types.ty().function([], []);
    let mut import_section = ImportSection::new();
    for (module, name) in imports {
        import_section.import(module, name, EntityType::Function(0));
    }
    let mut memories = MemorySection::new();
    memories.memory(MemoryType {
        minimum: (data.len() / PAGE + 1) as u64,
        maximum: None,
        memory64: false,
        shared: false,
        page_size_log2: None,
    });
    let mut exports = ExportSection::new();
    exports.export("memory", ExportKind::Memory, 0);
    let mut segments = DataSection::new();
    segments.active(0, &ConstExpr::i32_const(0), data);
    let mut module = Module::new();
    module
        .section(&types)
        .section(&import_section)
        .section(&memories)
        .section(&exports)
        .section(&segments);
    module.finish()
}

fn write_fixtures(dir: &Path) -> Result<(), Box<dyn Error>> {
    fs::create_dir_all(dir)?;
    fs::write(dir.join("evil.wasm"), guest(&[NOTIFY, SOCKET], EVIL, &[]))?;
    fs::write(dir.join("evil.toml"), EVIL_TOML)?;
    fs::write(dir.join("key.wasm"), guest(&[NOTIFY], KEY, &[]))?;
    fs::write(dir.join("key.toml"), KEY_TOML)?;
    println!("fixtures written to {}", dir.display());
    Ok(())
}

fn manifest_for(wasm: &Path, given: Option<PathBuf>) -> Result<PathBuf, Box<dyn Error>> {
    let sibling = wasm.with_extension("toml");
    let beside = wasm.with_file_name(MANIFEST_FILE);
    given
        .or_else(|| sibling.is_file().then_some(sibling))
        .or_else(|| beside.is_file().then_some(beside))
        .ok_or_else(|| {
            format!(
                "no manifest for {}, pass one after the wasm",
                wasm.display()
            )
            .into()
        })
}

fn state(import: &Import) -> String {
    match (import.capability, import.declared) {
        (Some(_), true) => "declared".to_owned(),
        (Some(c), false) => format!("needs {c}, not declared in capabilities"),
        (None, _) => "not offered by this desk".to_owned(),
    }
}

fn print(report: &ScanReport) {
    println!("size: {} bytes", report.size);
    println!(
        "producer: {}",
        report.producer.as_deref().unwrap_or("unknown")
    );
    if report.imports.is_empty() {
        println!("imports: none");
    }
    for import in &report.imports {
        let (shown, from) = match (import.module.as_str(), import.name.as_str()) {
            ("", name) | (name, "") => (name, String::new()),
            (module, name) => (name, format!(", from {module}")),
        };
        println!("imports: {shown} ({}){from}", state(import));
    }
    println!("exports: {}", report.exports.join(", "));
    println!("hosts: {}", report.hosts.join(", "));
    println!("urls: {}", report.urls.join(", "));
    if report.key_like.is_empty() {
        println!("key-like: none");
    }
    for key in &report.key_like {
        println!(
            "key-like: offset {}, starts {}, entropy {:.2} bits/char",
            key.offset, key.prefix, key.entropy
        );
    }
}

fn median_ms(wasm: &[u8], manifest: &Manifest) -> Result<(f64, Vec<f64>), Box<dyn Error>> {
    let mut runs = Vec::with_capacity(RUNS);
    for _ in 0..RUNS {
        let started = Instant::now();
        scan(wasm, manifest)?;
        runs.push(started.elapsed().as_secs_f64() * 1000.0);
    }
    let printed = runs.clone();
    runs.sort_by(f64::total_cmp);
    Ok((runs[RUNS / 2], printed))
}

fn bench(label: &str, wasm: &[u8], manifest: &Manifest) -> Result<(), Box<dyn Error>> {
    let (median, runs) = median_ms(wasm, manifest)?;
    let runs: Vec<String> = runs.iter().map(|r| format!("{r:.3}")).collect();
    println!(
        "bench: scan {label} ({} bytes), median of {RUNS}: {median:.3} ms (runs {})",
        wasm.len(),
        runs.join(", ")
    );
    Ok(())
}

fn main() -> Result<(), Box<dyn Error>> {
    let fixtures = Path::new(env!("CARGO_MANIFEST_DIR")).join("fixtures");
    let args: Vec<String> = std::env::args().skip(1).collect();
    let component = args.iter().any(|a| a == "--component");
    let mut paths = args
        .iter()
        .filter(|a| !a.starts_with("--"))
        .map(PathBuf::from);
    match (args.iter().any(|a| a == "--fixtures"), paths.next()) {
        (true, _) => write_fixtures(&fixtures),
        (false, Some(wasm)) => report(&wasm, paths.next(), component),
        (false, None) => {
            report(&fixtures.join("evil.wasm"), None, false)?;
            report(&fixtures.join("key.wasm"), None, false)
        }
    }
}

fn report(path: &Path, given: Option<PathBuf>, component: bool) -> Result<(), Box<dyn Error>> {
    let manifest_path = manifest_for(path, given)?;
    let manifest = Manifest::parse(&fs::read_to_string(&manifest_path)?)?;
    let mut wasm = fs::read(path)?;
    if component {
        wasm = ComponentEncoder::default()
            .validate(true)
            .module(&wasm)?
            .encode()?;
    }
    let declared: Vec<&str> = manifest.capabilities.keys().map(|c| c.name()).collect();
    println!(
        "scan {}{}",
        path.display(),
        if component {
            ", encoded as a component"
        } else {
            ""
        }
    );
    println!(
        "manifest: {} {}, capabilities: [{}]",
        manifest_path.display(),
        manifest.id,
        declared.join(", ")
    );
    print(&scan(&wasm, &manifest)?);
    bench(&path.display().to_string(), &wasm, &manifest)?;
    let pad: Vec<u8> = wasm.iter().copied().cycle().take(PAD_BYTES).collect();
    let padded = guest(&[NOTIFY], EVIL, &pad);
    let padded_report = scan(&padded, &manifest)?;
    println!(
        "padded guest: {} bytes, hosts [{}], {} key-like",
        padded_report.size,
        padded_report.hosts.join(", "),
        padded_report.key_like.len()
    );
    bench(
        "a guest padded with a 5 MB data segment",
        &padded,
        &manifest,
    )
}
