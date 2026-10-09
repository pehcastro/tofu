use std::error::Error;
use std::fs;
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use desk_plugin::{Cache, Event, Host, Installed, Plugin};
use wit_component::ComponentEncoder;

const COPIES: u8 = 10;

fn guest(name: &str) -> Result<(String, Vec<u8>), Box<dyn Error>> {
    let dir = Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("guests")
        .join(name);
    let core = fs::read(
        dir.join("target/wasm32-unknown-unknown/release")
            .join(format!("{name}.wasm")),
    )?;
    let component = ComponentEncoder::default()
        .validate(true)
        .module(&core)?
        .encode()?;
    Ok((fs::read_to_string(dir.join("plugin.toml"))?, component))
}

fn install(
    home: &Path,
    folder: &str,
    manifest: &str,
    wasm: &[u8],
    grant: Option<&str>,
) -> Result<(), Box<dyn Error>> {
    let dir = home.join(folder);
    fs::create_dir_all(&dir)?;
    fs::write(dir.join("plugin.toml"), manifest)?;
    fs::write(dir.join("plugin.wasm"), wasm)?;
    if let Some(grant) = grant {
        fs::write(dir.join("grant.json"), grant)?;
    }
    Ok(())
}

fn fresh(tag: &str) -> Result<PathBuf, Box<dyn Error>> {
    let nanos = SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos();
    let home = std::env::temp_dir().join(format!("desk-266-{tag}-{}-{nanos}", std::process::id()));
    fs::create_dir_all(&home)?;
    Ok(home)
}

fn ms(d: Duration) -> f64 {
    d.as_secs_f64() * 1000.0
}

fn load_all(host: &Host, plugins: &[Installed], label: &str) -> (Vec<Plugin>, Duration) {
    let mut loaded = Vec::new();
    let mut total = Duration::ZERO;
    for plugin in plugins {
        let started = Instant::now();
        let outcome = host.load(plugin);
        let took = started.elapsed();
        total += took;
        match outcome {
            Ok((p, cache)) => {
                let how = match cache {
                    Cache::Hit => "cache hit",
                    Cache::Miss => "compiled",
                };
                println!(
                    "{label} load {}: {:.3} ms, {how}",
                    plugin.manifest.id,
                    ms(took)
                );
                loaded.push(p);
            }
            Err(e) => println!("{label} load {}: {e}", plugin.manifest.id),
        }
    }
    (loaded, total)
}

fn find<'a>(plugins: &'a [Plugin], id: &str) -> Result<&'a Plugin, Box<dyn Error>> {
    plugins
        .iter()
        .find(|p| p.id().as_str() == id)
        .ok_or_else(|| format!("{id} did not load").into())
}

fn main() -> Result<(), Box<dyn Error>> {
    let (table_toml, table) = guest("table")?;
    let (rogue_toml, rogue) = guest("rogue")?;
    let home = fresh("drive")?;
    println!("plugins home, made fresh: {}", home.display());
    let as_id = |id: &str| table_toml.replacen("\"tofu.table\"", &format!("\"{id}\""), 1);
    let notify = Some(r#"{"granted":["notify"]}"#);
    install(&home, "tofu.table", &table_toml, &table, notify)?;
    fs::create_dir_all(home.join("tofu.table").join("notes"))?;
    install(&home, "tofu.rogue", &rogue_toml, &rogue, None)?;
    install(
        &home,
        "tofu.table-bare",
        &as_id("tofu.table-bare").replace("notify = true", "notify = false"),
        &table,
        None,
    )?;
    install(
        &home,
        "tofu.table-ungranted",
        &as_id("tofu.table-ungranted"),
        &table,
        Some(r#"{"granted":[]}"#),
    )?;
    install(&home, "tofu.con", &as_id("tofu.con"), &table, None)?;
    install(
        &home,
        "tofu.elsewhere",
        &as_id("tofu.mismatch"),
        &table,
        None,
    )?;

    let cold = Host::new(&home)?;
    let found = cold.discover(None);
    for refused in &found.refused {
        println!("discover refused: {refused}");
    }
    let ids: Vec<_> = found
        .plugins
        .iter()
        .map(|p| p.manifest.id.as_str())
        .collect();
    println!("discovered: {ids:?}");
    drop(load_all(&cold, &found.plugins, "cold"));

    let warm = Host::new(&home)?;
    let (plugins, _) = load_all(&warm, &warm.discover(None).plugins, "cached");

    let shown = find(&plugins, "tofu.table")?;
    let rows = shown.rows(0..30).wait()?;
    println!("tofu.table rows(0..30): {} nodes", rows.len());
    let clicked = shown.handle(Event::Click("r0".into()));
    println!("tofu.table handle(click r0): {:?}", clicked.wait());
    println!("tofu.table activity: {:?}", shown.take_activity().wait()?);

    let ungranted = find(&plugins, "tofu.table-ungranted")?;
    match ungranted.handle(Event::Click("r0".into())).wait() {
        Ok(()) => println!("tofu.table-ungranted handle(click r0): ok"),
        Err(e) => println!("tofu.table-ungranted handle(click r0): {e}"),
    }
    println!(
        "tofu.table-ungranted activity: {:?}",
        ungranted.take_activity().wait()?
    );

    let rogue = find(&plugins, "tofu.rogue")?;
    let started = Instant::now();
    let looping = rogue.rows(0..10);
    let view = shown.view().wait()?;
    println!(
        "tofu.table view while tofu.rogue loops: {} node, answered at {:.3} ms, rogue still running: {}",
        view.len(),
        ms(started.elapsed()),
        looping.try_take().is_none()
    );
    match looping.wait() {
        Ok(tree) => println!("tofu.rogue rows: {} nodes", tree.len()),
        Err(e) => println!(
            "tofu.rogue rows, after {:.1} ms: {e}",
            ms(started.elapsed())
        ),
    }

    let bench = fresh("bench")?;
    for copy in 0..COPIES {
        let id = format!("tofu.table-{copy}");
        let mut wasm = table.clone();
        wasm.extend_from_slice(&[0, 6, 4, b'c', b'o', b'p', b'y', copy]);
        install(&bench, &id, &as_id(&id), &wasm, notify)?;
    }
    let cold = Host::new(&bench)?;
    let (loaded, cold_total) = load_all(&cold, &cold.discover(None).plugins, "bench cold");
    drop(loaded);
    let warm = Host::new(&bench)?;
    let (loaded, warm_total) = load_all(&warm, &warm.discover(None).plugins, "bench cached");
    println!(
        "bench: {COPIES} copies of tofu.table, cold {:.3} ms total, cached {:.3} ms total, {} loaded cached",
        ms(cold_total),
        ms(warm_total),
        loaded.len()
    );

    fs::remove_dir_all(&home)?;
    fs::remove_dir_all(&bench)?;
    Ok(())
}
