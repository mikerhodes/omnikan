use crate::omnifocus::{self, Task};
use anyhow::Result;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;

#[derive(Clone, Copy, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "lowercase")]
pub enum Column {
    Backlog,
    Ready,
    Inprogress,
}

impl Column {
    pub fn as_tag(self) -> &'static str {
        match self {
            Self::Backlog => omnifocus::TAG_BACKLOG,
            Self::Ready => omnifocus::TAG_READY,
            Self::Inprogress => omnifocus::TAG_IN_PROGRESS,
        }
    }
}

fn column_for_task(task: &Task) -> Column {
    if task.tags.iter().any(|tag| tag == omnifocus::TAG_READY) {
        Column::Ready
    } else if task
        .tags
        .iter()
        .any(|tag| tag == omnifocus::TAG_IN_PROGRESS)
    {
        Column::Inprogress
    } else {
        Column::Backlog
    }
}

#[derive(Clone, Debug, Default, Serialize)]
pub struct Board {
    pub backlog: Vec<Task>,
    pub ready: Vec<Task>,
    pub inprogress: Vec<Task>,
}

impl Board {
    fn add(&mut self, task: Task, column: Column) {
        match column {
            Column::Backlog => self.backlog.push(task),
            Column::Ready => self.ready.push(task),
            Column::Inprogress => self.inprogress.push(task),
        }
    }
    fn remove(&mut self, id: &str) {
        self.backlog.retain(|task| task.id != id);
        self.ready.retain(|task| task.id != id);
        self.inprogress.retain(|task| task.id != id);
    }
    fn update(&mut self, task: Task) {
        for item in self
            .backlog
            .iter_mut()
            .chain(self.ready.iter_mut())
            .chain(self.inprogress.iter_mut())
        {
            if item.id == task.id {
                *item = task;
                return;
            }
        }
    }
}

pub struct WriteThroughCache {
    pub project_id: String,
    pub board: Board,
    pub tasks: HashMap<String, Task>,
}

impl WriteThroughCache {
    pub fn new(project_id: String) -> Self {
        Self {
            project_id,
            board: Board::default(),
            tasks: HashMap::new(),
        }
    }
    pub fn get_board(&self) -> Board {
        self.board.clone()
    }
    pub fn refresh(&mut self) -> Result<()> {
        let mut tasks = omnifocus::tasks_for_project(&self.project_id)?;
        tasks.sort_by(|a, b| b.added.cmp(&a.added));
        let mut board = Board::default();
        let mut map = HashMap::new();
        for task in tasks {
            let column = column_for_task(&task);
            map.insert(task.id.clone(), task.clone());
            board.add(task, column);
        }
        self.board = board;
        self.tasks = map;
        Ok(())
    }
    pub fn move_task(&mut self, id: &str, new_col: Column) -> Result<()> {
        let task = self
            .tasks
            .get(id)
            .cloned()
            .ok_or_else(|| anyhow::anyhow!("Invalid ID {id}"))?;
        let old_col = column_for_task(&task);
        if old_col == new_col {
            return Ok(());
        }
        omnifocus::swap_tag(id, old_col.as_tag(), new_col.as_tag())?;
        let updated = omnifocus::get_task(id)?;
        self.board.remove(id);
        self.board.add(updated.clone(), new_col);
        self.tasks.insert(id.to_string(), updated);
        Ok(())
    }
    pub fn delete_task(&mut self, id: &str) -> Result<()> {
        omnifocus::delete_task(id)?;
        self.board.remove(id);
        self.tasks.remove(id);
        Ok(())
    }
    pub fn complete_task(&mut self, id: &str) -> Result<()> {
        omnifocus::mark_complete(id)?;
        self.board.remove(id);
        self.tasks.remove(id);
        Ok(())
    }
    pub fn uncomplete_task(&mut self, id: &str) -> Result<()> {
        omnifocus::mark_incomplete(id)?;
        let task = omnifocus::get_task(id)?;
        self.tasks.insert(task.id.clone(), task.clone());
        self.board.add(task.clone(), column_for_task(&task));
        Ok(())
    }
    pub fn edit_task(&mut self, id: &str, name: &str, note: &str) -> Result<Task> {
        let task = omnifocus::edit_task(id, name, note)?;
        self.board.update(task.clone());
        if let Some(cached) = self.tasks.get_mut(id) {
            cached.name = task.name.clone();
            cached.note = task.note.clone();
        }
        Ok(task)
    }
    pub fn add_task(&mut self, name: &str, column: Column) -> Result<Task> {
        let task = omnifocus::add_task(name, column.as_tag(), &self.project_id)?;
        self.tasks.insert(task.id.clone(), task.clone());
        self.board.add(task.clone(), column);
        Ok(task)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn columns_serialize_with_api_names() {
        let board = Board {
            backlog: vec![],
            ready: vec![],
            inprogress: vec![],
        };
        assert_eq!(
            serde_json::to_string(&board).unwrap(),
            r#"{"backlog":[],"ready":[],"inprogress":[]}"#
        );
        assert_eq!(
            serde_json::to_string(&Column::Inprogress).unwrap(),
            r#""inprogress""#
        );
    }
}
