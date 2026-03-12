package omnifocus

import (
	"embed"
	"encoding/json"
	"log"
	"time"
)

var (
	//go:embed jxa
	jxa embed.FS
)

// Tags for the four Kanban columns.
const (
	TagBacklog    = "backlog"
	TagReady      = "ready"
	TagInProgress = "inprogress"
	TagDone       = "done"
)

// Task represents a task from OmniFocus.
type Task struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type tagQuery struct {
	Tag string `json:"tag"`
}

type swapTagArgs struct {
	ID     string `json:"id"`
	OldTag string `json:"oldTag"`
	NewTag string `json:"newTag"`
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

// TasksForTag returns all incomplete OmniFocus tasks that have the given tag.
func TasksForTag(tag string) ([]Task, error) {
	jsCode, _ := jxa.ReadFile("jxa/oftasksfortag.js")
	args, _ := json.Marshal(tagQuery{Tag: tag})

	log.Printf("omnifocus: querying tag %q", tag)
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

	return tasks, nil
}
