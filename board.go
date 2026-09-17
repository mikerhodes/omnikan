package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"

	"github.com/mikerhodes/omnikan/internal/omnifocus"
)

var tasksForProject = omnifocus.TasksForProject

// Column is a kanban column identifier.
type Column string

const (
	ColumnBacklog    Column = omnifocus.TagBacklog
	ColumnReady      Column = omnifocus.TagReady
	ColumnInProgress Column = omnifocus.TagInProgress
)

// parseColumn returns the Column for s, or false if s is not a valid column.
func parseColumn(s string) (Column, bool) {
	switch Column(s) {
	case ColumnBacklog, ColumnReady, ColumnInProgress:
		return Column(s), true
	}
	return "", false
}

func (c *Column) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	col, ok := parseColumn(s)
	if !ok {
		return fmt.Errorf("invalid column: %s", s)
	}
	*c = col
	return nil
}

//
// Main data store is a write-through cache to OmniFocus
//

type boardColumn []omnifocus.Task

func (s *boardColumn) addTask(t omnifocus.Task) {
	*s = append(*s, t)
}

func (s *boardColumn) removeTask(id string) {
	filtered := (*s)[:0]
	for _, t := range *s {
		if t.ID != id {
			filtered = append(filtered, t)
		}
	}
	*s = filtered
}

func (s *boardColumn) updateTask(t omnifocus.Task) {
	for i, task := range *s {
		if task.ID == t.ID {
			(*s)[i] = t
			break
		}
	}
}

type kanbanBoard struct {
	Backlog    boardColumn `json:"backlog"`
	Ready      boardColumn `json:"ready"`
	InProgress boardColumn `json:"inprogress"`
}

func newEmptyKanbanBoard() *kanbanBoard {
	return &kanbanBoard{
		Backlog:    []omnifocus.Task{},
		Ready:      []omnifocus.Task{},
		InProgress: []omnifocus.Task{},
	}
}

// columnForTask returns the kanban column for a task by inspecting its tags.
// Defaults to TagBacklog: all project tasks should be on the board.
func columnForTask(t *omnifocus.Task) Column {
	for _, tag := range t.Tags {
		if col, ok := parseColumn(tag); ok {
			return col
		}
	}
	return ColumnBacklog
}

// column returns a pointer to the board slice for the given column.
func (b *kanbanBoard) column(col Column) *boardColumn {
	switch col {
	case ColumnBacklog:
		return &b.Backlog
	case ColumnReady:
		return &b.Ready
	default:
		return &b.InProgress
	}
}

// writeThroughCache holds the in-memory board state and synchronises all
// mutations: every write calls OmniFocus first, then updates board and tasks
// on success, so the cache is never ahead of OmniFocus.
type writeThroughCache struct {
	projectID string

	// cacheMu protects board and tasks.
	cacheMu sync.Mutex
	board   *kanbanBoard
	tasks   map[string]*omnifocus.Task // task ID -> task
}

// getBoard returns the current cached board snapshot.
func (c *writeThroughCache) getBoard() *kanbanBoard {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	return c.board
}

func (c *writeThroughCache) setProjectID(projectID string) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	c.projectID = projectID
}

// refresh fetches all columns from OmniFocus and updates the cache.
func (c *writeThroughCache) refresh() error {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	newBoard := newEmptyKanbanBoard()
	newTasks := map[string]*omnifocus.Task{}

	tasks, err := tasksForProject(c.projectID)
	if err != nil {
		return fmt.Errorf("error getting tasks for project: %w", err)
	}
	// Display with newest at top
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Added > tasks[j].Added
	})
	for _, t := range tasks {
		newTasks[t.ID] = &t
		newBoard.column(columnForTask(&t)).addTask(t)
	}

	// Update cache on all successful
	c.board = newBoard
	c.tasks = newTasks

	log.Printf("board cache refreshed: %d backlog, %d ready, %d inprogress",
		len(c.board.Backlog), len(c.board.Ready), len(c.board.InProgress))
	return nil
}

// moveTask swaps the kanban tag on the task in OmniFocus and moves it to the
// target column in the cache. No-ops if the task is already in newCol.
func (c *writeThroughCache) moveTask(id string, newCol Column) error {

	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	ct, ok := c.tasks[id]
	if !ok {
		return fmt.Errorf("Invalid ID %s", id)
	}

	currentCol := columnForTask(ct)
	if currentCol == newCol {
		return nil
	}

	err := omnifocus.SwapTag(id, string(currentCol), string(newCol))
	if err != nil {
		return fmt.Errorf("swapping tag failed: %w", err)
	}

	// cached task is now out of date
	t, err := omnifocus.GetTask(id)
	if err != nil {
		return fmt.Errorf("swapping tag failed: %w", err)
	}
	c.tasks[id] = &t
	c.board.column(currentCol).removeTask(t.ID)
	c.board.column(newCol).addTask(t)

	return nil
}

// deleteTask deletes the task from OmniFocus and removes it from the cache.
func (c *writeThroughCache) deleteTask(id string) error {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	err := omnifocus.DeleteTask(id)
	if err != nil {
		return err
	}
	if t, ok := c.tasks[id]; ok {
		c.board.column(columnForTask(t)).removeTask(id)
		delete(c.tasks, id)
	}
	return nil
}

// completeTask marks the task complete in OmniFocus and removes it from the cache.
func (c *writeThroughCache) completeTask(id string) error {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	err := omnifocus.MarkComplete(id)
	if err != nil {
		return err
	}
	if t, ok := c.tasks[id]; ok {
		c.board.column(columnForTask(t)).removeTask(id)
		delete(c.tasks, id)
	}
	return nil
}

// uncompleteTask marks the task incomplete in OmniFocus
// and restores it to the cache.
func (c *writeThroughCache) uncompleteTask(id string) error {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	err := omnifocus.MarkIncomplete(id)
	if err != nil {
		return err
	}
	t, err := omnifocus.GetTask(id)
	if err != nil {
		return err
	}
	c.tasks[t.ID] = &t
	c.board.column(columnForTask(&t)).addTask(t)
	return nil
}

// editTask updates a task's name and note in OmniFocus and updates the cache.
func (c *writeThroughCache) editTask(id, name, note string) (*omnifocus.Task, error) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	task, err := omnifocus.EditTask(id, name, note)
	if err != nil {
		return nil, err
	}

	if t, ok := c.tasks[id]; ok {
		t.Name = task.Name
		t.Note = task.Note
		c.board.column(columnForTask(t)).updateTask(task)
	}

	return &task, nil
}

// addTask creates the task in OmniFocus and inserts it into the cache.
func (c *writeThroughCache) addTask(name string, col Column) (*omnifocus.Task, error) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	t, err := omnifocus.AddTask(name, string(col), c.projectID)
	if err != nil {
		return nil, err
	}
	c.tasks[t.ID] = &t
	c.board.column(col).addTask(t)
	return &t, nil
}
