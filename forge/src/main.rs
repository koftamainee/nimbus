mod cli;
mod cgroup;
mod config;
mod container;
mod image;
mod isolation;
mod log;

use std::process;

use clap::Parser;
use cli::{Cli, Commands};

fn main() -> anyhow::Result<()> {
    let cli = Cli::parse();

    if !nix::unistd::Uid::effective().is_root() {
        eprintln!("forge must be run as root");
        process::exit(1);
    }
    let data_dir = std::path::Path::new(&cli.data_dir);

    match &cli.command {
        Commands::Run {
            image,
            name,
            memory,
            cpus,
            env,
            cmd,
            detach,
        } => {
            let ec = container::run_container(
                data_dir,
                image,
                name,
                memory.as_deref(),
                *cpus,
                env,
                cmd,
                *detach,
            )?;
            if !detach {
                process::exit(ec);
            }
        }

        Commands::Ps { all } => {
            let containers = container::list_containers(data_dir, *all)?;
            if containers.is_empty() {
                println!("no containers");
            } else {
                println!(
                    "{:<13} {:<22} {:<14} {:<8} {}",
                    "CONTAINER ID", "NAME", "STATUS", "PID", "CREATED"
                );
                for c in &containers {
                    let short_name = &c.name[..12.min(c.name.len())];
                    let name = if c.name.len() > 20 {
                        format!("{}…", &c.name[..19])
                    } else {
                        c.name.clone()
                    };
                    let status = match c.status {
                        config::ContainerStatus::Created => "Created".into(),
                        config::ContainerStatus::Running => "Up".into(),
                        config::ContainerStatus::Exited => {
                            format!("Exited ({})", c.exit_code.unwrap_or(-1))
                        }
                    };
                    let pid_str = c.pid.map(|p| p.to_string()).unwrap_or_else(|| "-".into());
                    let created = &c.created_at[..19].replace('T', " ");
                    println!(
                        "{:<13} {:<22} {:<14} {:<8} {}",
                        short_name, name, status, pid_str, created
                    );
                }
            }
        }

        Commands::Stop { name, timeout } => {
            println!("stopping {}", name);
            container::stop_container(data_dir, name, *timeout)?;
            println!("stopped {}", name);
        }

        Commands::Start { name } => {
            let cname = container::start_container(data_dir, name)?;
            println!("{}", cname);
        }

        Commands::Kill { name } => {
            println!("killing {}", name);
            container::kill_container(data_dir, name)?;
            println!("killed {}", name);
        }

        Commands::Logs { name, tail, follow } => {
            log::read_logs(data_dir, name, *tail, *follow)?;
        }

        Commands::Rm { name } => {
            container::remove_container(data_dir, name)?;
            println!("removed {}", name);
        }
    }

    Ok(())
}
