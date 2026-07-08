mod cli;

use clap::Parser;
use cli::{Cli, Commands};
use hyper_util::rt::TokioIo;
use tonic::transport::{Endpoint, Uri};
use tower::service_fn;
use tokio::net::UnixStream;

pub mod nimbus {
    pub mod v1 {
        tonic::include_proto!("nimbus.v1");
    }
}

use nimbus::v1::forge_runtime_client::ForgeRuntimeClient;
use nimbus::v1::{
    KillRequest, ListRequest, LogsRequest, RemoveRequest, RunRequest, StartRequest, StopRequest,
};

async fn connect(socket: &str) -> anyhow::Result<ForgeRuntimeClient<tonic::transport::Channel>> {
    let socket = socket.to_string();
    let channel = Endpoint::try_from("http://[::]:0")?
        .connect_with_connector(service_fn(move |_: Uri| {
            let socket = socket.clone();
            async move {
                let stream = UnixStream::connect(&socket).await?;
                Ok::<_, std::io::Error>(TokioIo::new(stream))
            }
        }))
        .await?;
    Ok(ForgeRuntimeClient::new(channel))
}

const SOCKET_PATH: &str = "/var/run/forge.sock";

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let cli = Cli::parse();
    let mut client = connect(SOCKET_PATH).await?;

    match &cli.command {
        Commands::Run { image, name, memory, cpus, env, cmd, detach } => {
            let req = RunRequest {
                image: image.clone(),
                name: name.clone(),
                memory: memory.clone().unwrap_or_default(),
                cpus: cpus.unwrap_or(0.0),
                env: env.clone(),
                cmd: cmd.clone(),
            };
            let resp = client.run(req).await?.into_inner();
            if *detach {
                println!("{}", resp.name);
            }
        }
        Commands::Ps { all } => {
            let resp = client.list(ListRequest { all: *all }).await?.into_inner();
            if resp.containers.is_empty() {
                println!("no containers");
            } else {
                println!("{:<13} {:<22} {:<14} {:<8} {}", "CONTAINER ID", "NAME", "STATUS", "PID", "CREATED");
                for c in &resp.containers {
                    let short_name = c.name.chars().take(12).collect::<String>();
                    let name = if c.name.len() > 20 {
                        format!("{}…", &c.name[..19])
                    } else {
                        c.name.clone()
                    };
                    let status = match c.status.as_str() {
                        "Created" => "Created".into(),
                        "Running" => "Up".into(),
                        "Exited" => format!("Exited ({})", c.exit_code),
                        _ => c.status.clone(),
                    };
                    let pid_str = if c.pid == 0 { "-".into() } else { c.pid.to_string() };
                    let created = c.created_at[..19].replace('T', " ");
                    println!("{:<13} {:<22} {:<14} {:<8} {}", short_name, name, status, pid_str, created);
                }
            }
        }
        Commands::Stop { name, timeout } => {
            println!("stopping {}", name);
            client.stop(StopRequest { name: name.clone(), timeout: *timeout as i32 }).await?;
            println!("stopped {}", name);
        }
        Commands::Start { name } => {
            let resp = client.start(StartRequest { name: name.clone() }).await?.into_inner();
            println!("{}", resp.name);
        }
        Commands::Kill { name } => {
            println!("killing {}", name);
            client.kill(KillRequest { name: name.clone() }).await?;
            println!("killed {}", name);
        }
        Commands::Logs { name, tail, follow: _ } => {
            let resp = client.logs(LogsRequest { name: name.clone(), tail: tail.unwrap_or(0) as i32 }).await?.into_inner();
            print!("{}", resp.stdout);
            eprint!("{}", resp.stderr);
        }
        Commands::Rm { name } => {
            client.remove(RemoveRequest { name: name.clone() }).await?;
            println!("removed {}", name);
        }
    }

    Ok(())
}
