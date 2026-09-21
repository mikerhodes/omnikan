use crate::board::{Board, Column, WriteThroughCache};
use axum::{
    extract::{Query, State},
    http::StatusCode,
    response::IntoResponse,
    routing::{get, post},
    Json, Router,
};
use serde::Deserialize;
use std::convert::TryFrom;
use std::fmt;
use std::sync::{Arc, Mutex};
use tower_http::services::ServeDir;

pub type SharedCache = Arc<Mutex<WriteThroughCache>>;

#[derive(Debug)]
struct EmptyName;
impl fmt::Display for EmptyName {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("name is required")
    }
}

#[derive(Debug)]
struct EmptyTaskId;
impl fmt::Display for EmptyTaskId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("task ID is required")
    }
}

#[derive(Debug, Clone, Deserialize)]
#[serde(try_from = "String")]
struct TaskId(String);
impl TryFrom<String> for TaskId {
    type Error = EmptyTaskId;

    fn try_from(value: String) -> Result<Self, Self::Error> {
        if value.trim().is_empty() {
            return Err(EmptyTaskId);
        }
        Ok(Self(value))
    }
}
impl TaskId {
    fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, Deserialize)]
#[serde(try_from = "String")]
struct TaskName(String);
impl TryFrom<String> for TaskName {
    type Error = EmptyName;

    fn try_from(value: String) -> Result<Self, Self::Error> {
        if value.trim().is_empty() {
            return Err(EmptyName);
        }
        Ok(Self(value))
    }
}
impl TaskName {
    fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Deserialize)]
struct IdRequest {
    id: TaskId,
}
#[derive(Deserialize)]
struct MoveRequest {
    id: TaskId,
    #[serde(rename = "newCol")]
    new_col: Column,
}
#[derive(Deserialize)]
struct AddRequest {
    name: TaskName,
    col: Column,
}
#[derive(Deserialize)]
struct EditRequest {
    id: TaskId,
    name: String,
    note: String,
}
#[derive(Default, Deserialize)]
struct BoardQuery {
    #[serde(default)]
    force: bool,
}

async fn board(
    State(cache): State<SharedCache>,
    Query(query): Query<BoardQuery>,
) -> impl IntoResponse {
    if query.force {
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

async fn add_task(
    State(cache): State<SharedCache>,
    Json(req): Json<AddRequest>,
) -> impl IntoResponse {
    let result = tokio::task::spawn_blocking(move || {
        cache
            .lock()
            .map_err(|_| anyhow::anyhow!("cache lock poisoned"))
            .and_then(|mut c| c.add_task(req.name.as_str(), req.col))
    })
    .await;
    match result {
        Ok(Ok(task)) => Json(task).into_response(),
        _ => StatusCode::INTERNAL_SERVER_ERROR.into_response(),
    }
}

async fn edit_task(
    State(cache): State<SharedCache>,
    Json(req): Json<EditRequest>,
) -> impl IntoResponse {
    let result = tokio::task::spawn_blocking(move || {
        cache
            .lock()
            .map_err(|_| anyhow::anyhow!("cache lock poisoned"))
            .and_then(|mut c| c.edit_task(req.id.as_str(), &req.name, &req.note))
    })
    .await;
    match result {
        Ok(Ok(task)) => Json(task).into_response(),
        _ => StatusCode::INTERNAL_SERVER_ERROR.into_response(),
    }
}

async fn move_task(
    State(cache): State<SharedCache>,
    Json(req): Json<MoveRequest>,
) -> impl IntoResponse {
    operation(cache, move |cache| {
        cache.move_task(req.id.as_str(), req.new_col)
    })
    .await
}
async fn delete_task(
    State(cache): State<SharedCache>,
    Json(req): Json<IdRequest>,
) -> impl IntoResponse {
    operation(cache, move |cache| cache.delete_task(req.id.as_str())).await
}
async fn complete_task(
    State(cache): State<SharedCache>,
    Json(req): Json<IdRequest>,
) -> impl IntoResponse {
    operation(cache, move |cache| cache.complete_task(req.id.as_str())).await
}
async fn incomplete_task(
    State(cache): State<SharedCache>,
    Json(req): Json<IdRequest>,
) -> impl IntoResponse {
    operation(cache, move |cache| cache.uncomplete_task(req.id.as_str())).await
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
        .route("/api/delete", post(delete_task))
        .route("/api/complete", post(complete_task))
        .route("/api/incomplete", post(incomplete_task))
        .route("/api/add", post(add_task))
        .route("/api/edit", post(edit_task))
        .fallback_service(ServeDir::new("assets").append_index_html_on_directories(true))
        .with_state(cache)
}
