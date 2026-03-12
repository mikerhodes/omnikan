// Return all incomplete tasks having a given tag.
// Accepts { "tag": "kanban : inprogress" } as JSON in OSA_ARGS.
//
// Call it:
//   set -gx OSA_ARGS '{"tag": "kanban : inprogress"}'
//   osascript -l JavaScript oftasksfortag.js | jq .
//
// Returns JSON array:
// [
//   { "id": "iAKv1Uo8XqW", "name": "My task title" },
//   ...
// ]

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

    var tasks = ofDoc.flattenedTasks()
        .filter(function(t) { return t.completed() === false })
        .filter(function(t) {
            return t.tags().some(function(tag) { return tag.id() === ofTag.id() })
        })
        .map(function(t) { return { id: t.id(), name: t.name() } })

    JSON.stringify(tasks)
}
