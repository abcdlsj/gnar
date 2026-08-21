mod account;
mod app;
mod cli;
mod discover;
mod edge;
mod keys;
mod output;
mod protocol;
mod store;
mod tunnel;
mod ui;

use std::process::ExitCode;

use clap::Parser;
use tracing_subscriber::util::SubscriberInitExt;

use crate::app::{App, AppError};
use crate::cli::{Cli, Command};
use crate::edge::Edge;
use crate::output::Output;

#[tokio::main]
async fn main() -> ExitCode {
    let _ = tracing_subscriber::fmt()
        .with_max_level(tracing::Level::WARN)
        .with_target(false)
        .with_writer(std::io::stderr)
        .finish()
        .try_init();
    let cli = Cli::parse();
    let output = Output::new(cli.json, cli.no_tui);

    let result = match cli.command {
        Some(Command::Serve(args)) => Edge::new(args).run().await,
        Some(Command::Login(args)) => {
            if args.account.is_some() && !args.enrollment_key_stdin && !args.key_stdin {
                Err(AppError::Edge(
                    "--account requires --enrollment-key-stdin or --key-stdin".into(),
                ))
            } else {
                match account::command_edge(cli.edge.as_deref()) {
                    Ok(edge) if args.key_stdin => {
                        account::enroll_with_invite_stdin(&edge, args.account.as_deref(), &output)
                            .await
                    }
                    Ok(edge) if args.enrollment_key_stdin => {
                        let account = args.account.as_deref().unwrap_or_default();
                        account::enroll(&edge, account, &output).await
                    }
                    Ok(edge) => account::login(&edge).await,
                    Err(error) => Err(error),
                }
            }
        }
        Some(Command::Logout) => {
            account::command_edge(cli.edge.as_deref()).and_then(|edge| account::logout(&edge))
        }
        Some(Command::Whoami) => match account::command_edge(cli.edge.as_deref()) {
            Ok(edge) => account::whoami(&edge).await,
            Err(error) => Err(error),
        },
        Some(Command::Release { name }) => match account::command_edge(cli.edge.as_deref()) {
            Ok(edge) => account::release(&edge, &name).await,
            Err(error) => Err(error),
        },
        Some(Command::Key(args)) => keys::run(args, &output),
        Some(Command::Version) => {
            println!("gnar {}", env!("CARGO_PKG_VERSION"));
            Ok(())
        }
        None => {
            let settings = cli.settings();
            App::new(output)
                .run(cli.target, cli.edge, cli.name, settings)
                .await
        }
    };

    if let Err(error) = result {
        output.error(&error);
        return ExitCode::FAILURE;
    }
    ExitCode::SUCCESS
}
