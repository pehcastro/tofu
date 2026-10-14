use std::error::Error;
use std::fs;
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use desk_plugin::{GRANT_FILE, Home, Host, Id, MANIFEST_FILE, Manifest, Origin, Report, WASM_FILE};
use wit_component::ComponentEncoder;

const RUNS: usize = 5;

fn fresh(tag: &str) -> Result<PathBuf, Box<dyn Error>> {
    let nanos = SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos();
    let dir = std::env::temp_dir().join(format!("desk-307-{tag}-{}-{nanos}", std::process::id()));
    fs::create_dir_all(&dir)?;
    Ok(dir)
}

fn ms(d: Duration) -> f64 {
    d.as_secs_f64() * 1000.0
}

fn packed(folder: &Path) -> Result<(String, Vec<u8>), Box<dyn Error>> {
    let manifest = fs::read_to_string(folder.join(MANIFEST_FILE))?;
    let ready = folder.join(WASM_FILE);
    if ready.is_file() {
        return Ok((manifest, fs::read(ready)?));
    }
    let name = folder.file_name().ok_or("the folder has no name")?;
    let core = fs::read(
        folder
            .join("target/wasm32-unknown-unknown/release")
            .join(format!("{}.wasm", name.to_string_lossy())),
    )?;
    let wasm = ComponentEncoder::default()
        .validate(true)
        .module(&core)?
        .encode()?;
    Ok((manifest, wasm))
}

fn stage(root: &Path, id: &str, manifest: &str, wasm: &[u8]) -> Result<PathBuf, Box<dyn Error>> {
    let dir = root.join(id);
    fs::create_dir_all(&dir)?;
    fs::write(dir.join(MANIFEST_FILE), manifest)?;
    fs::write(dir.join(WASM_FILE), wasm)?;
    Ok(dir)
}

fn listing(dir: &Path) -> Result<String, Box<dyn Error>> {
    let mut names = fs::read_dir(dir)?
        .map(|e| Ok(e?.file_name().to_string_lossy().into_owned()))
        .collect::<Result<Vec<_>, std::io::Error>>()?;
    names.sort();
    Ok(names.join(", "))
}

fn print_report(report: &Report, took: Duration) -> Result<(), Box<dyn Error>> {
    let verb = match &report.replaced {
        Some(old) => format!("updated from {old} to"),
        None => "installed".to_owned(),
    };
    println!(
        "{verb} {} {} in {:.3} ms",
        report.id,
        report.version,
        ms(took)
    );
    println!("  sha256: {}", report.sha256);
    println!("  imports: {:?}", report.imports);
    println!("  dir: {}", report.dir.display());
    println!("  files: {}", listing(&report.dir)?);
    Ok(())
}

fn outcome(label: &str, result: Result<Report, desk_plugin::Error>) {
    match result {
        Ok(report) => println!("{label}: installed {} {}", report.id, report.version),
        Err(e) => println!("{label}: {e}"),
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let folder = PathBuf::from(
        std::env::args()
            .nth(1)
            .ok_or("usage: install <plugin folder>")?,
    );
    let (text, wasm) = packed(&folder)?;
    let id = Manifest::parse(&text)?.id;
    let guests = fs::canonicalize(Path::new(env!("CARGO_MANIFEST_DIR")).join("guests"))?;
    let origin = if fs::canonicalize(&folder)?.starts_with(&guests) {
        Origin::BuiltIn
    } else {
        Origin::Person
    };
    let staging = fresh("stage")?;
    let staged = stage(&staging, id.as_str(), &text, &wasm)?;

    let home = Home::find()?;
    println!("plugins home: {}", home.plugins.display());
    println!("cache: {}", home.cache.display());
    println!("origin: {origin:?}");
    let host = Host::new(&home)?;
    let started = Instant::now();
    match host.install(&staged, origin) {
        Ok(report) => print_report(&report, started.elapsed())?,
        Err(e) => println!("install {id}: {e}"),
    }

    let name = id.name();
    let as_id = |to: &str| text.replacen(&format!("\"{id}\""), &format!("\"{to}\""), 1);
    let demo_root = fresh("demo")?;
    let demo = Host::new(&Home::at(&demo_root))?;
    let person = format!("luiz.{name}");
    let tofu = format!("tofu.{name}");
    let local = format!("local.{name}");
    let bare = format!("luiz.{name}-bare");
    outcome(
        &format!("{tofu} by a person"),
        demo.install(
            &stage(&staging, &tofu, &as_id(&tofu), &wasm)?,
            Origin::Person,
        ),
    );
    let person_dir = stage(&staging, &person, &as_id(&person), &wasm)?;
    outcome(&person, demo.install(&person_dir, Origin::Person));
    outcome(
        &format!("{person} again"),
        demo.install(&person_dir, Origin::Person),
    );
    let started = Instant::now();
    match demo.update(&person_dir, Origin::Person) {
        Ok(report) => print_report(&report, started.elapsed())?,
        Err(e) => println!("{person} update: {e}"),
    }
    outcome(
        &local,
        demo.install(
            &stage(&staging, &local, &as_id(&local), &wasm)?,
            Origin::Person,
        ),
    );
    if text.contains("notify = true") {
        let undeclared = as_id(&bare).replace("notify = true", "notify = false");
        outcome(
            &format!("{bare}, notify undeclared"),
            demo.install(&stage(&staging, &bare, &undeclared, &wasm)?, Origin::Person),
        );
    } else {
        println!("{bare}: skipped, {MANIFEST_FILE} declares no notify to take away");
    }
    let installed = Home::at(&demo_root).plugins.join(&person);
    fs::write(
        installed.join(MANIFEST_FILE),
        as_id(&format!("acme.{name}")),
    )?;
    outcome(
        &format!("{person} over a folder holding acme.{name}"),
        demo.install(&person_dir, Origin::Person),
    );
    fs::write(installed.join(MANIFEST_FILE), as_id(&person))?;
    fs::create_dir_all(installed.join("notes"))?;
    fs::write(installed.join("notes").join("a.md"), "kept")?;
    demo.uninstall(&Id::parse(&person)?, true)?;
    println!("{person} uninstalled, data kept: {}", listing(&installed)?);

    let project = fresh("project")?;
    let shipped = stage(&project, &person, &as_id(&person), &wasm)?;
    fs::write(
        shipped.join(GRANT_FILE),
        r#"{"version":2,"modes":{"notify":"allow"}}"#,
    )?;
    for plugin in demo.discover(Some(&project)).plugins {
        let loaded = match demo.load(&plugin) {
            Ok(_) => "loaded".to_owned(),
            Err(e) => e.to_string(),
        };
        println!(
            "project {}: {:?}, modes {:?}, load: {loaded}",
            plugin.manifest.id, plugin.status, plugin.modes
        );
    }

    let mut runs = Vec::with_capacity(RUNS);
    for run in 0..RUNS {
        let root = fresh(&format!("bench-{run}"))?;
        let host = Host::new(&Home::at(&root))?;
        let started = Instant::now();
        host.install(&staged, origin)?;
        runs.push(ms(started.elapsed()));
        fs::remove_dir_all(&root)?;
    }
    let printed: Vec<String> = runs.iter().map(|r| format!("{r:.3}")).collect();
    runs.sort_by(f64::total_cmp);
    println!(
        "bench: install {id} into a fresh home, cold cache, median of {RUNS}: {:.3} ms (runs {})",
        runs[RUNS / 2],
        printed.join(", ")
    );

    fs::remove_dir_all(&staging)?;
    fs::remove_dir_all(&demo_root)?;
    fs::remove_dir_all(&project)?;
    Ok(())
}
