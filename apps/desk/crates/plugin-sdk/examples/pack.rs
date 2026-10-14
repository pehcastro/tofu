use std::error::Error;
use std::fs;

use wit_component::ComponentEncoder;

fn main() -> Result<(), Box<dyn Error>> {
    let mut args = std::env::args_os().skip(1);
    let (Some(core), Some(out)) = (args.next(), args.next()) else {
        return Err("usage: pack <core.wasm> <plugin.wasm>".into());
    };
    let module = fs::read(&core)?;
    let component = ComponentEncoder::default()
        .validate(true)
        .module(&module)?
        .encode()?;
    fs::write(&out, component)?;
    Ok(())
}
