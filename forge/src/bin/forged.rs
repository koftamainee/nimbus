use std::path::PathBuf;
use std::pin::Pin;
use std::task::{Context, Poll};

use anyhow::Context as _;
use clap::Parser;
use futures_core::Stream;
use tokio::net::UnixListener;
use tonic::transport::Server;
use tonic::{Request, Response, Status};

use forge::container;
use forge::log;

pub mod nimbus {
    pub mod v1 {
        tonic::include_proto!("nimbus.v1");
    }
}

use nimbus::v1::forge_runtime_server::{ForgeRuntime, ForgeRuntimeServer};
use nimbus::v1::{
    Empty, InspectRequest, InspectResponse, KillRequest, ListRequest, ListResponse,
    LogsRequest, LogsResponse, RemoveRequest, RunRequest, RunResponse,
    StartRequest, StartResponse, StopRequest,
};

struct Incoming {
    listener: UnixListener,
}

impl Stream for Incoming {
    type Item = Result<tokio::net::UnixStream, std::io::Error>;

    fn poll_next(self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<Option<Self::Item>> {
        match self.listener.poll_accept(cx) {
            Poll::Ready(Ok((stream, _))) => Poll::Ready(Some(Ok(stream))),
            Poll::Ready(Err(e)) => Poll::Ready(Some(Err(e))),
            Poll::Pending => Poll::Pending,
        }
    }
}

#[derive(Parser)]
#[command(name = "forged", version, about = "Forge runtime daemon")]
struct ForgedCli {
    #[arg(long, default_value = "/var/lib/forge")]
    data_dir: String,
    #[arg(long, default_value = "/var/run/forge.sock")]
    socket: String,
    #[arg(long, default_value = "http://127.0.0.1:11111")]
    registry_addr: String,
}

struct ForgedService {
    data_dir: PathBuf,
    registry_addr: String,
}

#[tonic::async_trait]
impl ForgeRuntime for ForgedService {
    async fn run(&self, req: Request<RunRequest>) -> Result<Response<RunResponse>, Status> {
        let r = req.into_inner();
        let ec = container::run_container(
            &self.data_dir,
            &r.image,
            Some(&self.registry_addr),
            &r.name,
            if r.memory.is_empty() { None } else { Some(r.memory.as_str()) },
            if r.cpus == 0.0 { None } else { Some(r.cpus) },
            &r.env,
            &r.cmd,
            true,
        )
        .await
        .map_err(|e| Status::internal(e.to_string()))?;
        if ec != 0 {
            eprintln!("container {} exited with code {}", r.name, ec);
        }
        Ok(Response::new(RunResponse { name: r.name }))
    }

    async fn start(&self, req: Request<StartRequest>) -> Result<Response<StartResponse>, Status> {
        let r = req.into_inner();
        let name = container::start_container(&self.data_dir, &r.name)
            .map_err(|e| Status::internal(e.to_string()))?;
        Ok(Response::new(StartResponse { name }))
    }

    async fn stop(&self, req: Request<StopRequest>) -> Result<Response<Empty>, Status> {
        let r = req.into_inner();
        container::stop_container(&self.data_dir, &r.name, r.timeout as u64)
            .map_err(|e| Status::internal(e.to_string()))?;
        Ok(Response::new(Empty {}))
    }

    async fn kill(&self, req: Request<KillRequest>) -> Result<Response<Empty>, Status> {
        let r = req.into_inner();
        container::kill_container(&self.data_dir, &r.name)
            .map_err(|e| Status::internal(e.to_string()))?;
        Ok(Response::new(Empty {}))
    }

    async fn remove(&self, req: Request<RemoveRequest>) -> Result<Response<Empty>, Status> {
        let r = req.into_inner();
        container::remove_container(&self.data_dir, &r.name)
            .map_err(|e| Status::internal(e.to_string()))?;
        Ok(Response::new(Empty {}))
    }

    async fn inspect(&self, req: Request<InspectRequest>) -> Result<Response<InspectResponse>, Status> {
        let r = req.into_inner();
        let config = container::list_containers(&self.data_dir, true)
            .map_err(|e| Status::internal(e.to_string()))?
            .into_iter()
            .find(|c| c.name == r.name)
            .ok_or_else(|| Status::not_found(format!("container '{}' not found", r.name)))?;
        Ok(Response::new(InspectResponse {
            name: config.name,
            status: format!("{:?}", config.status),
            pid: config.pid.unwrap_or(0) as i32,
            exit_code: config.exit_code.unwrap_or(0),
            created_at: config.created_at,
            started_at: config.started_at.unwrap_or_default(),
            finished_at: config.finished_at.unwrap_or_default(),
            image: config.image,
        }))
    }

    async fn list(&self, req: Request<ListRequest>) -> Result<Response<ListResponse>, Status> {
        let all = req.into_inner().all;
        let containers = container::list_containers(&self.data_dir, all)
            .map_err(|e| Status::internal(e.to_string()))?
            .into_iter()
            .map(|c| InspectResponse {
                name: c.name,
                status: format!("{:?}", c.status),
                pid: c.pid.unwrap_or(0) as i32,
                exit_code: c.exit_code.unwrap_or(0),
                created_at: c.created_at,
                started_at: c.started_at.unwrap_or_default(),
                finished_at: c.finished_at.unwrap_or_default(),
                image: c.image,
            })
            .collect();
        Ok(Response::new(ListResponse { containers }))
    }

    async fn logs(&self, req: Request<LogsRequest>) -> Result<Response<LogsResponse>, Status> {
        let r = req.into_inner();
        let tail = if r.tail == 0 { None } else { Some(r.tail as usize) };
        let (stdout, stderr) = log::read_logs_buf(&self.data_dir, &r.name, tail)
            .map_err(|e| Status::internal(e.to_string()))?;
        Ok(Response::new(LogsResponse {
            stdout: String::from_utf8_lossy(&stdout).to_string(),
            stderr: String::from_utf8_lossy(&stderr).to_string(),
        }))
    }
}

fn cleanup_socket(path: &str) {
    let _ = std::fs::remove_file(path);
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let cli = ForgedCli::parse();

    if !nix::unistd::Uid::effective().is_root() {
        eprintln!("forged must be run as root");
        std::process::exit(1);
    }

    cleanup_socket(&cli.socket);

    let data_dir = PathBuf::from(&cli.data_dir);
    std::fs::create_dir_all(&data_dir)?;
    std::fs::create_dir_all(data_dir.join("containers"))?;
    std::fs::create_dir_all(data_dir.join("images"))?;

    let listener = UnixListener::bind(&cli.socket)
        .context("bind unix socket")?;

    let svc = ForgedService { data_dir, registry_addr: cli.registry_addr };
    let incoming = Incoming { listener };

    eprintln!("forged listening on {}", cli.socket);

    Server::builder()
        .add_service(ForgeRuntimeServer::new(svc))
        .serve_with_incoming(incoming)
        .await?;

    Ok(())
}
