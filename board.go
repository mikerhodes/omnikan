package main

import (
	"fmt"
	"log"
	"sort"
	"sync"

	"github.com/mikerhodes/omnikan/internal/omnifocus"
)

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

// columnForTask returns the kanban column for a task by inspecting its tags.
// Returns the first tag that matches a kanban column constant, or empty string.
func columnForTask(t *omnifocus.Task) string {
	for _, tag := range t.Tags {
		if tag == omnifocus.TagBacklog || tag == omnifocus.TagReady || tag == omnifocus.TagInProgress {
			return tag
		}
	}
	// Policy choice: all project tasks should be on board.
	return omnifocus.TagBacklog
}

// isValidColumn returns true for the valid kanban columns.
func isValidColumn(col string) bool {
	return col == omnifocus.TagBacklog ||
		col == omnifocus.TagReady ||
		col == omnifocus.TagInProgress
}

// column returns a pointer to the board slice for the given column.
func (b *kanbanBoard) column(col string) *boardColumn {
	switch col {
	case omnifocus.TagBacklog:
		return &b.Backlog
	case omnifocus.TagReady:
		return &b.Ready
	case omnifocus.TagInProgress:
		return &b.InProgress
	}
	return nil
}

// moveTask moves a task from one column slice to another in the board.
func (b *kanbanBoard) moveTask(t omnifocus.Task, fromCol, toCol string) {
	if s := b.column(fromCol); s != nil {
		s.removeTask(t.ID)
	}
	if s := b.column(toCol); s != nil {
		s.addTask(t)
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

// refresh fetches all columns from OmniFocus and updates the cache.
func (c *writeThroughCache) refresh() error {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	newBoard := kanbanBoard{
		Backlog:    []omnifocus.Task{},
		Ready:      []omnifocus.Task{},
		InProgress: []omnifocus.Task{},
	}
	newTasks := map[string]*omnifocus.Task{}

	tasks, err := omnifocus.TasksForProject(c.projectID)
	if err != nil {
		return fmt.Errorf("error getting tasks for project: %w", err)
	}
	// Display with newest at top
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Added > tasks[j].Added
	})
	for _, t := range tasks {
		newTasks[t.ID] = &t
		col := columnForTask(&t)
		switch col {
		case omnifocus.TagBacklog:
			newBoard.Backlog = append(newBoard.Backlog, t)
		case omnifocus.TagReady:
			newBoard.Ready = append(newBoard.Ready, t)
		case omnifocus.TagInProgress:
			newBoard.InProgress = append(newBoard.InProgress, t)
		default:
			panic("Task with unknown tag; should never happen")
		}
	}

	// Update cache on all successful
	c.board = &newBoard
	c.tasks = newTasks

	log.Printf("board cache refreshed: %d backlog, %d ready, %d inprogress",
		len(c.board.Backlog), len(c.board.Ready), len(c.board.InProgress))
	return nil
}

// moveTask swaps the kanban tag on the task in OmniFocus and moves it to the
// target column in the cache. No-ops if the task is already in newCol.
func (c *writeThroughCache) moveTask(id string, newCol string) error {
	if !isValidColumn(newCol) {
		return fmt.Errorf("invalid column name %s", newCol)
	}

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

	err := omnifocus.SwapTag(id, currentCol, newCol)
	if err != nil {
		return fmt.Errorf("swapping tag failed: %w", err)
	}

	// cached task is now out of date
	t, err := omnifocus.GetTask(id)
	if err != nil {
		return fmt.Errorf("swapping tag failed: %w", err)
	}
	c.tasks[id] = &t
	c.board.moveTask(t, currentCol, newCol)

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
func (c *writeThroughCache) addTask(name string, col string) (*omnifocus.Task, error) {
	if name == "" || !isValidColumn(col) {
		return nil, fmt.Errorf("invalid column %s", col)
	}

	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	t, err := omnifocus.AddTask(name, col, c.projectID)
	if err != nil {
		return nil, err
	}
	c.tasks[t.ID] = &t
	c.board.column(col).addTask(t)
	return &t, nil
}
