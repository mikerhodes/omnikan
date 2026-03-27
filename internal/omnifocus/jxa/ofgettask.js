// Return a single task by ID.
// Accepts { "id": "abc123" } via OSA_ARGS.
// Returns { "id": "...", "name": "...", "note": "...", "added": "..." }
// Exits with a non-zero status if the task is not found.
//
// Call it:
//   set -gx OSA_ARGS '{"id":"abc123"}'
//   osascript -l JavaScript ofgettask.js | jq .

ObjC.import('stdlib')
var args = JSON.parse($.getenv('OSA_ARGS'))

// @ts-ignore
var ofApp = Application("OmniFocus")


var script = `
    let id = ${JSON.stringify(args.id)};
    let t = Task.byIdentifier(id)
    if (t === null) {
        throw new Error("task not found: " + id)
    }
    JSON.stringify({
        id: t.id.primaryKey,
        name: t.name,
        note: t.note,
        added: t.added,
        tags: t.tags.map(function(tg) { return tg.name })
    })
`

ofApp.evaluateJavascript(script);
