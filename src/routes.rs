use crate::board::{Column, WriteThroughCache};
use axum::{
    extract::{Query, State},
    http::StatusCode,
    response::{IntoResponse, Response},
    routing::{get, post},
    Json, Router,
};
use serde::{Deserialize, Deserializer};
use std::sync::{Arc, Mutex};
use tower_http::services::ServeDir;

pub type SharedCache = Arc<Mutex<WriteThroughCache>>;

fn non_empty<'de, D>(d: D) -> Result<String, D::Error>
where
    D: Deserializer<'de>,
{
    let value = String::deserialize(d)?;
    if value.trim().is_empty() {
        return Err(serde::de::Error::custom("must not be empty"));
    }

    Ok(value)
}

#[derive(Deserialize)]
struct IdRequest {
    #[serde(deserialize_with = "non_empty")]
    id: String,
}
#[derive(Deserialize)]
struct MoveRequest {
    #[serde(deserialize_with = "non_empty")]
    id: String,
    #[serde(rename = "newCol")]
    new_col: Column,
}
#[derive(Deserialize)]
struct AddRequest {
    #[serde(deserialize_with = "non_empty")]
    name: String,
    col: Column,
}
#[derive(Deserialize)]
struct EditRequest {
    #[serde(deserialize_with = "non_empty")]
    id: String,
    #[serde(deserialize_with = "non_empty")]
    name: String,
    note: String,
}
#[derive(Default, Deserialize)]
struct BoardQuery {
    #[serde(default)]
    force: bool,
}

async fn board(State(cache): State<SharedCache>, Query(query): Query<BoardQuery>) -> Response {
    tokio::task::spawn_blocking(move || {
        let Ok(mut cache) = cache.lock() else {
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        };
        if query.force && cache.refresh().is_err() {
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
        Json(cache.get_board()).into_response()
    })
    .await
    .unwrap_or(StatusCode::INTERNAL_SERVER_ERROR.into_response())
}

async fn add_task(State(cache): State<SharedCache>, Json(req): Json<AddRequest>) -> Response {
    tokio::task::spawn_blocking(move || {
        let Ok(mut cache) = cache.lock() else {
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        };
        let result = cache.add_task(req.name.as_str(), req.col);
        match result {
            Ok(task) => Json(task).into_response(),
            Err(_) => StatusCode::INTERNAL_SERVER_ERROR.into_response(),
        }
    })
    .await
    .unwrap_or(StatusCode::INTERNAL_SERVER_ERROR.into_response())
}

async fn edit_task(State(cache): State<SharedCache>, Json(req): Json<EditRequest>) -> Response {
    tokio::task::spawn_blocking(move || {
        let Ok(mut cache) = cache.lock() else {
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        };
        let result = cache.edit_task(req.id.as_str(), &req.name, &req.note);
        match result {
            Ok(task) => Json(task).into_response(),
            Err(_) => StatusCode::INTERNAL_SERVER_ERROR.into_response(),
        }
    })
    .await
    .unwrap_or(StatusCode::INTERNAL_SERVER_ERROR.into_response())
}

async fn move_task(State(cache): State<SharedCache>, Json(req): Json<MoveRequest>) -> StatusCode {
    tokio::task::spawn_blocking(move || {
        let Ok(mut cache) = cache.lock() else {
            return StatusCode::INTERNAL_SERVER_ERROR;
        };
        match cache.move_task(req.id.as_str(), req.new_col) {
            Ok(()) => StatusCode::NO_CONTENT,
            Err(_) => StatusCode::INTERNAL_SERVER_ERROR,
        }
    })
    .await
    .unwrap_or(StatusCode::INTERNAL_SERVER_ERROR)
}

async fn delete_task(State(cache): State<SharedCache>, Json(req): Json<IdRequest>) -> StatusCode {
    tokio::task::spawn_blocking(move || {
        let Ok(mut cache) = cache.lock() else {
            return StatusCode::INTERNAL_SERVER_ERROR;
        };
        match cache.delete_task(req.id.as_str()) {
            Ok(()) => StatusCode::NO_CONTENT,
            Err(_) => StatusCode::INTERNAL_SERVER_ERROR,
        }
    })
    .await
    .unwrap_or(StatusCode::INTERNAL_SERVER_ERROR)
}

async fn complete_task(State(cache): State<SharedCache>, Json(req): Json<IdRequest>) -> StatusCode {
    tokio::task::spawn_blocking(move || {
        let Ok(mut cache) = cache.lock() else {
            return StatusCode::INTERNAL_SERVER_ERROR;
        };
        match cache.complete_task(req.id.as_str()) {
            Ok(()) => StatusCode::NO_CONTENT,
            Err(_) => StatusCode::INTERNAL_SERVER_ERROR,
        }
    })
    .await
    .unwrap_or(StatusCode::INTERNAL_SERVER_ERROR)
}

async fn incomplete_task(
    State(cache): State<SharedCache>,
    Json(req): Json<IdRequest>,
) -> StatusCode {
    tokio::task::spawn_blocking(move || {
        let Ok(mut cache) = cache.lock() else {
            return StatusCode::INTERNAL_SERVER_ERROR;
        };
        match cache.uncomplete_task(req.id.as_str()) {
            Ok(()) => StatusCode::NO_CONTENT,
            Err(_) => StatusCode::INTERNAL_SERVER_ERROR,
        }
    })
    .await
    .unwrap_or(StatusCode::INTERNAL_SERVER_ERROR)
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
