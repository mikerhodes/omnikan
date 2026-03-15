// Return all incomplete tasks that have a given tag and belong to a given project.
// Accepts { "tag": "backlog", "projectId": "abc123" } as JSON in OSA_ARGS.
//
// Call it:
//   set -gx OSA_ARGS '{"tag": "backlog", "projectId": "abc123"}'
//   osascript -l JavaScript oftasksfortag.js | jq .
//
// Returns JSON array:
// [
//   { "id": "iAKv1Uo8XqW", "name": "My task title", "note": "..." },
//   ...
// ]
//
// Uses tag.tasks() as the starting point (fast: ~0.35s for a small tag) and
// then filters by containingProject().id(). This is faster than starting from
// project.tasks() and filtering by tag, because tag.tasks() already returns a
// small set and each containingProject() call is cheap.

ObjC.import('stdlib')
var args = JSON.parse($.getenv('OSA_ARGS'))

// @ts-ignore
var ofApp = Application("OmniFocus")
var ofDoc = ofApp.defaultDocument

var matchingTags = ofDoc.flattenedTags.whose({ name: args.tag })
if (matchingTags.length === 0) {
    JSON.stringify([])
} else {
    var ofTag = matchingTags()[0]

    var tasks = ofTag.tasks()
        .filter(function(t) {
            if (t.completed() || t.dropped()) return false
            var proj = t.containingProject()
            return proj && proj.id() === args.projectId
        })
        .map(function(t) { return { id: t.id(), name: t.name(), note: t.note() } })

    JSON.stringify(tasks)
}
