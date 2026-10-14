use desk_control::transport::{Server, runtime_dir};
use desk_control::{ErrorCode, Failure};
use std::io::BufRead;

fn main() -> std::io::Result<()> {
    let server = Server::start(env!("CARGO_PKG_VERSION"), None, |request| {
        Err(Failure::new(
            Some(request.id),
            ErrorCode::DeskNotRunning,
            "serve example answers only desk.ping and desk.initialize".to_owned(),
            None,
        ))
    })?;
    let instance = server.instance();
    println!("endpoint {}", instance.endpoint);
    if let Some(dir) = runtime_dir() {
        println!(
            "instance {}",
            dir.join(format!("instance-{}.json", instance.pid))
                .display()
        );
    }
    println!("press Enter to stop");
    let mut line = String::new();
    std::io::stdin().lock().read_line(&mut line)?;
    drop(server);
    println!("stopped");
    Ok(())
}
