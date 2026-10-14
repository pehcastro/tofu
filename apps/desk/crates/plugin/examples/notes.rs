use std::error::Error;
use std::fs;
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use desk_plugin::{Event, Folder, Home, Host, Id, Kind, MANIFEST_FILE, Node, Origin, WASM_FILE};
use wit_component::ComponentEncoder;

const RUNS: usize = 20;
const MEGABYTE: usize = 1 << 20;

fn ms(d: Duration) -> f64 {
    d.as_secs_f64() * 1000.0
}

fn component(core: &Path) -> Result<Vec<u8>, Box<dyn Error>> {
    let core = fs::read(core).map_err(|e| format!("{}: {e}", core.display()))?;
    Ok(ComponentEncoder::default()
        .validate(true)
        .module(&core)?
        .encode()?)
}

fn stage(root: &Path, manifest: &str, wasm: &[u8]) -> Result<PathBuf, Box<dyn Error>> {
    let id = manifest
        .lines()
        .find_map(|l| l.strip_prefix("id = \""))
        .and_then(|l| l.strip_suffix('"'))
        .ok_or("plugin.toml has no id")?;
    let dir = root.join(id);
    fs::create_dir_all(&dir)?;
    fs::write(dir.join(MANIFEST_FILE), manifest)?;
    fs::write(dir.join(WASM_FILE), wasm)?;
    Ok(dir)
}

fn kinds(tree: &[Node]) -> Vec<String> {
    tree.iter()
        .map(|node| match &node.kind {
            Kind::Editor(e) => format!(
                "{}=editor({}, wrap {}, syntax {})",
                node.key, e.path, e.wrap, e.syntax
            ),
            Kind::Text(t) => format!("{}=text({t})", node.key),
            Kind::Button(b) => format!("{}=button({b})", node.key),
            Kind::Markdown(m) => format!("{}=markdown({} bytes)", node.key, m.len()),
            _ => format!("{}=other", node.key),
        })
        .collect()
}

fn status(tree: &[Node]) -> String {
    tree.iter()
        .find_map(|node| match &node.kind {
            Kind::Text(t) if node.key == "status" => Some(t.clone()),
            _ => None,
        })
        .unwrap_or_default()
}

fn percentile(sorted: &[f64], p: usize) -> f64 {
    let at = (sorted.len().saturating_sub(1) * p) / 100;
    sorted.get(at).copied().unwrap_or(f64::NAN)
}

fn spread(mut samples: Vec<f64>) -> String {
    samples.sort_by(f64::total_cmp);
    format!(
        "p50 {:.3} ms, p95 {:.3} ms over {}",
        percentile(&samples, 50),
        percentile(&samples, 95),
        samples.len()
    )
}

fn main() -> Result<(), Box<dyn Error>> {
    let crate_dir = Path::new(env!("CARGO_MANIFEST_DIR"));
    let notes_dir = crate_dir.join("../../examples/plugins/notes-lite");
    let notes =
        component(&notes_dir.join("target/wasm32-unknown-unknown/release/notes_lite.wasm"))?;
    let table_dir = crate_dir.join("guests/table");
    let table = component(&table_dir.join("target/wasm32-unknown-unknown/release/table.wasm"))?;

    let nanos = SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos();
    let tofu = std::env::temp_dir().join(format!("desk-348-{}-{nanos}", std::process::id()));
    let home = Home::at(&tofu);
    fs::create_dir_all(&tofu)?;
    fs::write(tofu.join("settings.json"), "{\"secret\": true}")?;
    println!("home, made fresh: {}", tofu.display());

    let staging = tofu.join("staging");
    let notes_src = stage(
        &staging,
        &fs::read_to_string(notes_dir.join(MANIFEST_FILE))?,
        &notes,
    )?;
    let table_src = stage(
        &staging,
        &fs::read_to_string(table_dir.join(MANIFEST_FILE))?.replacen(
            "\"tofu.table\"",
            "\"local.table\"",
            1,
        ),
        &table,
    )?;
    let host = Host::new(&home)?;
    for src in [&notes_src, &table_src] {
        let report = host.install(src, Origin::Person)?;
        println!(
            "installed {} {}, imports [{}]",
            report.id,
            report.version,
            report.imports.join(", ")
        );
    }

    let found = host.discover(None);
    for refused in &found.refused {
        println!("discover refused: {refused}");
    }
    let mut loaded = Vec::new();
    for plugin in &found.plugins {
        match host.load(plugin) {
            Ok((running, _)) => {
                println!("loaded {} on {:?}", plugin.manifest.id, plugin.manifest.api);
                loaded.push(running);
            }
            Err(e) => println!("load {}: {e}", plugin.manifest.id),
        }
    }
    let find = |id: &str| loaded.iter().find(|p| p.id().as_str() == id);
    let lite = find("local.notes-lite").ok_or("local.notes-lite did not load")?;
    let old = find("local.table").ok_or("local.table did not load")?;

    let tree = lite.view().wait()?;
    println!("notes-lite view: {}", kinds(&tree).join(" | "));
    let note = home.plugins.join("local.notes-lite/notes/a.md");
    println!("notes/a.md on disk: {}", note.is_file());
    println!(
        "desk@0.1 table view: {} nodes, rows(0..3): {} nodes",
        old.view().wait()?.len(),
        old.rows(0..3).wait()?.len()
    );
    old.handle(Event::Saved("ignored by 0.1".into())).wait()?;
    println!("desk@0.1 table handle(saved): ok, not delivered");

    lite.handle(Event::Click("escape".into())).wait()?;
    println!(
        "after read ../../settings.json: {}",
        status(&lite.view().wait()?)
    );
    lite.handle(Event::Click("files".into())).wait()?;
    println!("after List notes/: {}", status(&lite.view().wait()?));
    lite.handle(Event::Saved("notes/a.md".into())).wait()?;
    println!("after saved(notes/a.md): {}", status(&lite.view().wait()?));
    println!("activity: {:?}", lite.take_activity().wait()?);

    let folder = Folder::new(
        Id::parse("local.notes-lite")?,
        home.plugins.join("local.notes-lite"),
    );
    for asked in [
        "notes/a.md",
        "../../settings.json",
        "notes/../../x",
        "C:/Windows/win.ini",
        "/etc/passwd",
        "CON",
        "notes/nul.txt",
        "notes/COM1.md",
        "notes/a.md:hidden",
        "notes/trailing. ",
        "plugin.toml",
        "Grant.JSON",
    ] {
        match folder.read(asked) {
            Ok(bytes) => println!("read {asked}: ok, {} bytes", bytes.len()),
            Err(e) => println!("read {asked}: {e}"),
        }
    }
    let link = home.plugins.join("local.notes-lite/notes/out");
    #[cfg(windows)]
    let linked = std::os::windows::fs::symlink_dir(&tofu, &link);
    #[cfg(unix)]
    let linked = std::os::unix::fs::symlink(&tofu, &link);
    match linked {
        Ok(()) => match folder.read("notes/out/settings.json") {
            Ok(bytes) => println!("read through a link out: ok, {} bytes", bytes.len()),
            Err(e) => println!("read through a link out: {e}"),
        },
        Err(e) => println!("read through a link out: skipped, the link was not made: {e}"),
    }
    println!(
        "write grant.json: {}",
        folder
            .write("grant.json", b"{}")
            .err()
            .map_or("ok".to_owned(), |e| e.to_string())
    );

    let mut host_writes = Vec::new();
    let block = vec![b'x'; MEGABYTE];
    for _ in 0..RUNS {
        let started = Instant::now();
        folder.write("notes/bench.bin", &block)?;
        host_writes.push(ms(started.elapsed()));
    }
    let mut guest_writes = Vec::new();
    for _ in 0..RUNS {
        let started = Instant::now();
        lite.handle(Event::Click("big".into())).wait()?;
        guest_writes.push(ms(started.elapsed()));
    }
    println!(
        "bench fs.write 1 MB, host Folder::write: {}",
        spread(host_writes)
    );
    println!(
        "bench fs.write 1 MB, guest click to written (list<u8> across the boundary): {}",
        spread(guest_writes)
    );
    println!("guest status: {}", status(&lite.view().wait()?));

    drop(loaded);
    fs::remove_dir_all(&tofu)?;
    Ok(())
}
