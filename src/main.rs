mod board;
mod omnifocus;
mod routes;

use anyhow::{Context, Result};
use board::WriteThroughCache;
use clap::Parser;
use std::sync::{Arc, Mutex};
use std::time::Duration;
use tokio::net::TcpListener;

#[derive(Parser)]
struct Args {
    #[arg(long)]
    project: String,
    #[arg(long, default_value = "localhost:8080")]
    addr: String,
}

#[tokio::main]
async fn main() -> Result<()> {
    let args = Args::parse_from(std::env::args().map(|arg| match arg.as_str() {
        "-project" => "--project".to_string(),
        "-addr" => "--addr".to_string(),
        value if value.starts_with("-project=") => format!("-{value}"),
        value if value.starts_with("-addr=") => format!("-{value}"),
        value => value.to_string(),
    }));
    let project_id =
        tokio::task::spawn_blocking(move || omnifocus::project_id(&args.project)).await??;
    let cache = Arc::new(Mutex::new(WriteThroughCache::new(project_id)));
    {
        let cache = Arc::clone(&cache);
        tokio::task::spawn_blocking(move || cache.lock().unwrap().refresh()).await??;
    }
    let refresh_cache = Arc::clone(&cache);
    tokio::spawn(async move {
        let mut interval = tokio::time::interval(Duration::from_secs(600));
        interval.tick().await;
        loop {
            interval.tick().await;
            let cache = Arc::clone(&refresh_cache);
            let _ = tokio::task::spawn_blocking(move || {
                cache
                    .lock()
                    .map_err(|_| anyhow::anyhow!("cache lock poisoned"))
                    .and_then(|mut c| c.refresh())
            })
            .await;
        }
    });
    let listener = TcpListener::bind(&args.addr)
        .await
        .context("binding HTTP address")?;
    axum::serve(listener, routes::router(cache))
        .with_graceful_shutdown(shutdown_signal())
        .await
        .context("serving HTTP")?;
    Ok(())
}

async fn shutdown_signal() {
    let _ = tokio::signal::ctrl_c().await;
}
