use crate::board::{Board, Column, WriteThroughCache};
use axum::{
    extract::{Query, State},
    http::StatusCode,
    response::IntoResponse,
    routing::{get, post},
    Json, Router,
};
use serde::Deserialize;
use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use tower_http::services::ServeDir;

pub type SharedCache = Arc<Mutex<WriteThroughCache>>;

#[derive(Deserialize)]
struct IdRequest {
    id: String,
}
#[derive(Deserialize)]
struct MoveRequest {
    id: String,
    #[serde(rename = "newCol")]
    new_col: Column,
}
#[derive(Deserialize)]
struct AddRequest {
    name: String,
    col: Column,
}
#[derive(Deserialize)]
struct EditRequest {
    id: String,
    name: String,
    note: String,
}

async fn board(
    State(cache): State<SharedCache>,
    Query(query): Query<HashMap<String, String>>,
) -> impl IntoResponse {
    if query.get("force").is_some_and(|value| value == "true") {
        let result = tokio::task::spawn_blocking(move || {
            let mut cache = cache
                .lock()
                .map_err(|_| anyhow::anyhow!("cache lock poisoned"))?;
            cache.refresh()?;
            Ok::<Board, anyhow::Error>(cache.get_board())
        })
        .await;
        return match result {
            Ok(Ok(board)) => Json(board).into_response(),
            _ => StatusCode::INTERNAL_SERVER_ERROR.into_response(),
        };
    }
    match cache.lock() {
        Ok(cache) => Json(cache.get_board()).into_response(),
        Err(_) => StatusCode::INTERNAL_SERVER_ERROR.into_response(),
    }
}
async fn move_task(
    State(cache): State<SharedCache>,
    Json(req): Json<MoveRequest>,
) -> impl IntoResponse {
    operation(cache, move |cache| cache.move_task(&req.id, req.new_col)).await
}
async fn delete(State(cache): State<SharedCache>, Json(req): Json<IdRequest>) -> impl IntoResponse {
    operation(cache, move |cache| cache.delete_task(&req.id)).await
}
async fn complete(
    State(cache): State<SharedCache>,
    Json(req): Json<IdRequest>,
) -> impl IntoResponse {
    operation(cache, move |cache| cache.complete_task(&req.id)).await
}
async fn incomplete(
    State(cache): State<SharedCache>,
    Json(req): Json<IdRequest>,
) -> impl IntoResponse {
    operation(cache, move |cache| cache.uncomplete_task(&req.id)).await
}
async fn add(State(cache): State<SharedCache>, Json(req): Json<AddRequest>) -> impl IntoResponse {
    if req.name.is_empty() {
        return (StatusCode::BAD_REQUEST, "name is required").into_response();
    }
    let result = tokio::task::spawn_blocking(move || {
        cache
            .lock()
            .map_err(|_| anyhow::anyhow!("cache lock poisoned"))
            .and_then(|mut c| c.add_task(&req.name, req.col))
    })
    .await;
    match result {
        Ok(Ok(task)) => Json(task).into_response(),
        _ => StatusCode::INTERNAL_SERVER_ERROR.into_response(),
    }
}
async fn edit(State(cache): State<SharedCache>, Json(req): Json<EditRequest>) -> impl IntoResponse {
    let result = tokio::task::spawn_blocking(move || {
        cache
            .lock()
            .map_err(|_| anyhow::anyhow!("cache lock poisoned"))
            .and_then(|mut c| c.edit_task(&req.id, &req.name, &req.note))
    })
    .await;
    match result {
        Ok(Ok(task)) => Json(task).into_response(),
        _ => StatusCode::INTERNAL_SERVER_ERROR.into_response(),
    }
}
async fn operation<F>(cache: SharedCache, f: F) -> axum::response::Response
where
    F: FnOnce(&mut WriteThroughCache) -> anyhow::Result<()> + Send + 'static,
{
    let result = tokio::task::spawn_blocking(move || {
        cache
            .lock()
            .map_err(|_| anyhow::anyhow!("cache lock poisoned"))
            .and_then(|mut c| f(&mut c))
    })
    .await;
    match result {
        Ok(Ok(())) => StatusCode::NO_CONTENT.into_response(),
        _ => StatusCode::INTERNAL_SERVER_ERROR.into_response(),
    }
}

pub fn router(cache: SharedCache) -> Router {
    Router::new()
        .route("/api/board", get(board))
        .route("/api/move", post(move_task))
        .route("/api/delete", post(delete))
        .route("/api/complete", post(complete))
        .route("/api/incomplete", post(incomplete))
        .route("/api/add", post(add))
        .route("/api/edit", post(edit))
        .fallback_service(ServeDir::new("assets").append_index_html_on_directories(true))
        .with_state(cache)
}
