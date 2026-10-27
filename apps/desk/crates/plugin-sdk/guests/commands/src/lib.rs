use desk_plugin_sdk::{Command, Event, Part, Plugin, command, stack, text};

#[derive(Default)]
struct Commands {
    last: String,
}

impl Plugin for Commands {
    fn view(&self) -> Part {
        stack(
            "commands",
            [text("last", format!("Last run: {}", self.last))],
        )
    }

    fn handle(&mut self, _event: Event) {}

    fn commands(&self) -> Vec<Command> {
        vec![command("commands.echo", "Echo the args")]
    }

    fn run(&mut self, id: &str, args: &str) -> Result<String, String> {
        match id {
            "commands.echo" => {
                self.last = args.to_owned();
                Ok(args.to_owned())
            }
            other => Err(format!("no command {other}")),
        }
    }
}

desk_plugin_sdk::plugin!(Commands);
