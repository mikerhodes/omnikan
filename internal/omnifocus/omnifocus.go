package omnifocus

import (
	"embed"
	"encoding/json"
	"log"
	"sort"
	"time"
)

var (
	//go:embed jxa
	jxa embed.FS
)

// ProjectName is the OmniFocus project tasks must belong to.
const ProjectName = "🤘Cloudant Next"

// Tags for the three Kanban columns.
const (
	TagBacklog    = "backlog"
	TagReady      = "ready"
	TagInProgress = "inprogress"
)

// Task represents a task from OmniFocus.
type Task struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Note  string `json:"note"`
	Added string `json:"added"`
}

type projectNameQuery struct {
	ProjectName string `json:"projectName"`
}

type projectIDResult struct {
	ID string `json:"id"`
}

type tagQuery struct {
	Tag       string `json:"tag"`
	ProjectID string `json:"projectId"`
}

type swapTagArgs struct {
	ID     string `json:"id"`
	OldTag string `json:"oldTag"`
	NewTag string `json:"newTag"`
}

type taskIDArgs struct {
	ID string `json:"id"`
}

type addTaskArgs struct {
	Name      string `json:"name"`
	Tag       string `json:"tag"`
	ProjectID string `json:"projectId"`
}

// MarkComplete marks a task as complete in OmniFocus.
func MarkComplete(id string) error {
	jsCode, _ := jxa.ReadFile("jxa/ofmarktaskcomplete.js")
	args, _ := json.Marshal(taskIDArgs{ID: id})

	log.Printf("omnifocus: marking task %q complete", id)
	start := time.Now()
	_, err := executeScript(jsCode, args)
	if err != nil {
		log.Printf("omnifocus: mark complete error after %s: %v", time.Since(start), err)
		return err
	}
	log.Printf("omnifocus: mark complete done in %s", time.Since(start))
	return nil
}

// MarkIncomplete marks a task as incomplete in OmniFocus.
func MarkIncomplete(id string) error {
	jsCode, _ := jxa.ReadFile("jxa/ofmarktaskincomplete.js")
	args, _ := json.Marshal(taskIDArgs{ID: id})

	log.Printf("omnifocus: marking task %q incomplete", id)
	start := time.Now()
	_, err := executeScript(jsCode, args)
	if err != nil {
		log.Printf("omnifocus: mark incomplete error after %s: %v", time.Since(start), err)
		return err
	}
	log.Printf("omnifocus: mark incomplete done in %s", time.Since(start))
	return nil
}

// SwapTag removes oldTag from a task and adds newTag.
func SwapTag(id, oldTag, newTag string) error {
	jsCode, _ := jxa.ReadFile("jxa/ofswaptag.js")
	args, _ := json.Marshal(swapTagArgs{ID: id, OldTag: oldTag, NewTag: newTag})

	log.Printf("omnifocus: swapping tag %q -> %q on task %q", oldTag, newTag, id)
	start := time.Now()
	_, err := executeScript(jsCode, args)
	if err != nil {
		log.Printf("omnifocus: swap tag error after %s: %v", time.Since(start), err)
		return err
	}
	log.Printf("omnifocus: swap tag done in %s", time.Since(start))
	return nil
}

// ProjectID looks up the OmniFocus ID for a project by name.
func ProjectID(projectName string) (string, error) {
	jsCode, _ := jxa.ReadFile("jxa/ofprojectid.js")
	args, _ := json.Marshal(projectNameQuery{ProjectName: projectName})

	log.Printf("omnifocus: looking up project %q", projectName)
	start := time.Now()
	out, err := executeScript(jsCode, args)
	if err != nil {
		log.Printf("omnifocus: project lookup error after %s: %v", time.Since(start), err)
		return "", err
	}
	log.Printf("omnifocus: project lookup done in %s", time.Since(start))

	var result projectIDResult
	if err := json.Unmarshal(out, &result); err != nil {
		return "", err
	}
	return result.ID, nil
}

// AddTask creates a new task in the given project with the given tag.
func AddTask(name, tag, projectID string) (Task, error) {
	jsCode, _ := jxa.ReadFile("jxa/ofaddtask.js")
	args, _ := json.Marshal(addTaskArgs{Name: name, Tag: tag, ProjectID: projectID})

	log.Printf("omnifocus: adding task %q to tag %q in project %q", name, tag, projectID)
	start := time.Now()
	out, err := executeScript(jsCode, args)
	if err != nil {
		log.Printf("omnifocus: add task error after %s: %v", time.Since(start), err)
		return Task{}, err
	}
	log.Printf("omnifocus: add task done in %s", time.Since(start))

	var task Task
	if err := json.Unmarshal(out, &task); err != nil {
		return Task{}, err
	}
	return task, nil
}

// TasksForTag returns all incomplete tasks in the given project that have the given tag.
func TasksForTag(tag, projectID string) ([]Task, error) {
	jsCode, _ := jxa.ReadFile("jxa/oftasksfortag.js")
	args, _ := json.Marshal(tagQuery{Tag: tag, ProjectID: projectID})

	log.Printf("omnifocus: querying tag %q in project %q", tag, projectID)
	start := time.Now()
	out, err := executeScript(jsCode, args)
	if err != nil {
		log.Printf("omnifocus: tag %q error after %s: %v", tag, time.Since(start), err)
		return nil, err
	}
	log.Printf("omnifocus: tag %q returned %d bytes in %s", tag, len(out), time.Since(start))

	var tasks []Task
	if err := json.Unmarshal(out, &tasks); err != nil {
		return nil, err
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Added > tasks[j].Added
	})

	return tasks, nil
}
