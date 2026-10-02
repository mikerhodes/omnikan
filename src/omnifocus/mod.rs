mod embed;
mod jxa;

use anyhow::Result;
use serde::{Deserialize, Serialize};

pub const TAG_BACKLOG: &str = "backlog";
pub const TAG_READY: &str = "ready";
pub const TAG_IN_PROGRESS: &str = "inprogress";

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
pub struct Task {
    pub id: String,
    pub name: String,
    pub note: String,
    #[serde(default)]
    pub added: String,
    #[serde(default)]
    pub tags: Vec<String>,
}

fn run<T: Serialize, R: for<'de> Deserialize<'de>>(script: &[u8], args: &T) -> Result<R> {
    let output = jxa::execute_script(script, &serde_json::to_vec(args)?)?;
    Ok(serde_json::from_slice(&output)?)
}

#[derive(Serialize)]
struct ProjectName<'a> {
    #[serde(rename = "projectName")]
    project_name: &'a str,
}
#[derive(Deserialize)]
struct ProjectId {
    id: String,
}
#[derive(Serialize)]
struct TaskId<'a> {
    id: &'a str,
}
#[derive(Serialize)]
struct Edit<'a> {
    id: &'a str,
    name: &'a str,
    note: &'a str,
}
#[derive(Serialize)]
struct Add<'a> {
    name: &'a str,
    tag: &'a str,
    #[serde(rename = "projectId")]
    project_id: &'a str,
}
#[derive(Serialize)]
struct Swap<'a> {
    id: &'a str,
    #[serde(rename = "oldTag")]
    old_tag: &'a str,
    #[serde(rename = "newTag")]
    new_tag: &'a str,
}
#[derive(Serialize)]
struct Project<'a> {
    projectid: &'a str,
}

pub fn project_id(name: &str) -> Result<String> {
    Ok(run::<_, ProjectId>(embed::OFPROJECTID, &ProjectName { project_name: name })?.id)
}
pub fn tasks_for_project(id: &str) -> Result<Vec<Task>> {
    run(embed::OFTASKSFORPROJECT, &Project { projectid: id })
}
pub fn get_task(id: &str) -> Result<Task> {
    run(embed::OFGETTASK, &TaskId { id })
}
pub fn add_task(name: &str, tag: &str, project_id: &str) -> Result<Task> {
    run(
        embed::OFADDTASK,
        &Add {
            name,
            tag,
            project_id,
        },
    )
}
pub fn edit_task(id: &str, name: &str, note: &str) -> Result<Task> {
    run(embed::OFEDITTASK, &Edit { id, name, note })
}
pub fn swap_tag(id: &str, old_tag: &str, new_tag: &str) -> Result<()> {
    let _: serde_json::Value = run(
        embed::OFSWAPTAG,
        &Swap {
            id,
            old_tag,
            new_tag,
        },
    )?;
    Ok(())
}
pub fn mark_complete(id: &str) -> Result<()> {
    let _: serde_json::Value = run(embed::OFMARKTASKSCOMPLETE, &TaskId { id })?;
    Ok(())
}
pub fn mark_incomplete(id: &str) -> Result<()> {
    let _: serde_json::Value = run(embed::OFMARKTASKINCOMPLETE, &TaskId { id })?;
    Ok(())
}
pub fn delete_task(id: &str) -> Result<()> {
    let _: serde_json::Value = run(embed::OFDELETETASK, &TaskId { id })?;
    Ok(())
}
