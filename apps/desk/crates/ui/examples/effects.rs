use std::error::Error;
use std::path::Path;
use std::sync::Arc;
use std::time::{Duration, Instant};

use desk_ui::effects::{
    Effect, EffectCache, EffectKey, EffectParams, Lookup, Source, Stage, apply, cover,
    to_render_image,
};

const STREET: Source = Source {
    name: "street",
    bytes: include_bytes!("../../../assets/intro/street.jpg"),
};
const WIDTH: u32 = 1440;
const HEIGHT: u32 = 900;
const HITS: u32 = 1000;

const EFFECTS: [(&str, Stage); 6] = [
    (
        "duotone",
        Stage {
            effect: Effect::Duotone {
                shadow: [22, 12, 34],
                light: [240, 200, 146],
            },
            strength: 255,
        },
    ),
    (
        "glow",
        Stage {
            effect: Effect::Glow {
                threshold: 150,
                radius: 6,
            },
            strength: 255,
        },
    ),
    (
        "halftone",
        Stage {
            effect: Effect::Halftone { cell: 6 },
            strength: 150,
        },
    ),
    (
        "dither",
        Stage {
            effect: Effect::Dither { levels: 6 },
            strength: 210,
        },
    ),
    (
        "scanlines",
        Stage {
            effect: Effect::Scanlines { period: 3 },
            strength: 90,
        },
    ),
    (
        "grain",
        Stage {
            effect: Effect::Grain {
                amount: 14,
                seed: 7,
            },
            strength: 255,
        },
    ),
];

fn ms(elapsed: Duration) -> f64 {
    elapsed.as_secs_f64() * 1000.0
}

fn main() -> Result<(), Box<dyn Error>> {
    let out = std::env::args()
        .nth(1)
        .ok_or("usage: effects <folder for the PNGs>")?;
    run(Path::new(&out))
}

fn run(out: &Path) -> Result<(), Box<dyn Error>> {
    std::fs::create_dir_all(out)?;

    let started = Instant::now();
    let source = image::load_from_memory(STREET.bytes)?.into_rgba8();
    let decoded = started.elapsed();
    let started = Instant::now();
    let fitted = cover(&source, WIDTH, HEIGHT)?;
    let fit = started.elapsed();
    println!(
        "street.jpg {}x{} to {WIDTH}x{HEIGHT}",
        source.width(),
        source.height()
    );
    println!("{:<10} {:>8.2} ms", "decode", ms(decoded));
    println!("{:<10} {:>8.2} ms", "fit", ms(fit));
    fitted.save(out.join("source.png"))?;

    for (name, stage) in EFFECTS {
        let mut image = fitted.clone();
        let params = EffectParams {
            stages: vec![stage],
        };
        let started = Instant::now();
        apply(&mut image, WIDTH, HEIGHT, &params);
        println!("{name:<10} {:>8.2} ms", ms(started.elapsed()));
        image.save(out.join(format!("{name}.png")))?;
    }

    let params = EffectParams {
        stages: EFFECTS.iter().map(|(_, stage)| *stage).collect(),
    };
    let mut stacked = fitted.clone();
    let started = Instant::now();
    apply(&mut stacked, WIDTH, HEIGHT, &params);
    println!("{:<10} {:>8.2} ms", "stack", ms(started.elapsed()));
    stacked.save(out.join("stack.png"))?;

    let key = EffectKey {
        source: STREET,
        width: WIDTH,
        height: HEIGHT,
        params,
    };
    let mut cache = EffectCache::default();
    cache.insert(
        key.clone(),
        Lookup::Ready(Arc::new(to_render_image(stacked))),
    );
    let started = Instant::now();
    let mut found = 0;
    for _ in 0..HITS {
        if let Some(Lookup::Ready(_)) = cache.get(&key) {
            found += 1;
        }
    }
    let each = started.elapsed() / HITS;
    if found != HITS {
        return Err(format!("the cache answered {found} of {HITS} lookups").into());
    }
    println!("{:<10} {:>8.4} ms, mean of {HITS}", "cache hit", ms(each));
    println!("wrote {}", out.display());
    Ok(())
}

#[cfg(test)]
mod drive {
    use std::error::Error;
    use std::path::{Path, PathBuf};

    const SHOTS: &str = "../../../../.local/desk-app/shots/desk-309";

    #[test]
    fn writes_the_shots_and_compares_them_with_the_reviewed_ones() -> Result<(), Box<dyn Error>> {
        let out = PathBuf::from(std::env::var("TMPDIR")?).join("desk-309");
        super::run(&out)?;
        let shots = Path::new(env!("CARGO_MANIFEST_DIR")).join(SHOTS);
        let names = super::EFFECTS.iter().map(|(name, _)| *name);
        for name in ["source", "stack"].into_iter().chain(names) {
            let file = format!("{name}.png");
            let Ok(reviewed) = image::open(shots.join(&file)) else {
                println!("{name:<10} no reviewed shot at {}", shots.display());
                continue;
            };
            let now = image::open(out.join(&file))?.into_rgba8();
            let differences: Vec<u8> = now
                .as_raw()
                .iter()
                .zip(reviewed.into_rgba8().as_raw())
                .map(|(a, b)| a.abs_diff(*b))
                .collect();
            let total: u64 = differences.iter().copied().map(u64::from).sum();
            let over = differences.iter().filter(|d| **d > 8).count();
            println!(
                "{name:<10} mean {:.3}, max {}, over 8 in {:.3}% of channels",
                total as f64 / differences.len() as f64,
                differences.iter().max().copied().unwrap_or(0),
                over as f64 * 100.0 / differences.len() as f64
            );
        }
        Ok(())
    }
}
