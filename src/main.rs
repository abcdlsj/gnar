mod account;
mod app;
mod cli;
mod discover;
mod edge;
mod output;
mod protocol;
mod store;
mod tunnel;
mod ui;

use std::process::ExitCode;

use clap::Parser;

use crate::app::App;
use crate::cli::{Cli, Command};
use crate::edge::Edge;
use crate::output::Output;

#[tokio::main]
async fn main() -> ExitCode {
    let cli = Cli::parse();
    let output = Output::new(cli.json, cli.no_tui);

    let result = match cli.command {
        Some(Command::Serve(args)) => Edge::new(args).run().await,
        Some(Command::Login) => account::login(&cli.edge).await,
        Some(Command::Logout) => account::logout(&cli.edge),
        Some(Command::Whoami) => account::whoami(&cli.edge).await,
        Some(Command::Release { name }) => account::release(&cli.edge, &name).await,
        Some(Command::Version) => {
            println!("gnar {}", env!("CARGO_PKG_VERSION"));
            Ok(())
        }
        None => App::new(output).run(cli.target, cli.edge, cli.name).await,
    };

    if let Err(error) = result {
        output.error(&error);
        return ExitCode::FAILURE;
    }
    ExitCode::SUCCESS
}
