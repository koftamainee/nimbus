use clap::{Parser, Subcommand};

#[derive(Parser)]
#[command(name = "forge", version, about = "Container runtime")]
pub struct Cli {
    #[arg(long, default_value = "/var/lib/forge", global = true)]
    pub data_dir: String,

    #[command(subcommand)]
    pub command: Commands,
}

#[derive(Subcommand)]
pub enum Commands {
    #[command(about = "Run a container from an image")]
    Run {
        #[arg(long, help = "Path to image tar file (.tar or .tar.gz)")]
        image: String,
        #[arg(long, help = "Container name")]
        name: String,
        #[arg(long, help = "Memory limit (e.g. 512m, 1g)")]
        memory: Option<String>,
        #[arg(long, help = "CPU limit (e.g. 1.5)")]
        cpus: Option<f64>,
        #[arg(long, help = "Environment variables (KEY=value)")]
        env: Vec<String>,
        #[arg(short = 'd', long = "detach", help = "Run container in background")]
        detach: bool,
        #[arg(trailing_var_arg = true, allow_hyphen_values = true)]
        cmd: Vec<String>,
    },
    #[command(about = "List containers")]
    Ps {
        #[arg(long, short = 'a', help = "Show all containers (default shows only running)")]
        all: bool,
    },
    #[command(about = "Stop a running container")]
    Stop {
        name: String,
        #[arg(long, default_value = "10", help = "Grace period before SIGKILL (seconds)")]
        timeout: u64,
    },
    #[command(about = "Start a stopped container")]
    Start {
        name: String,
    },
    #[command(about = "Kill a running container (SIGKILL)")]
    Kill {
        name: String,
    },
    #[command(about = "Show container logs")]
    Logs {
        name: String,
        #[arg(long, help = "Show last N lines")]
        tail: Option<usize>,
        #[arg(long, short = 'f', help = "Follow log output")]
        follow: bool,
    },
    #[command(about = "Remove a stopped container")]
    Rm {
        name: String,
    },
}
