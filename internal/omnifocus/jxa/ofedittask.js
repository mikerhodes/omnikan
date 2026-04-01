// Edit a task's name and note by ID.
// Accepts { "id": "abc123", "name": "New Name", "note": "New Note" } via OSA_ARGS.
// Returns { "id": "...", "name": "...", "note": "..." }
// Exits with a non-zero status if the task is not found.
//
// Call it:
//   set -gx OSA_ARGS '{"id":"abc123","name":"Updated Name","note":"Updated Note"}'
//   osascript -l JavaScript ofedittask.js | jq .

ObjC.import('stdlib')
var args = JSON.parse($.getenv('OSA_ARGS'))

// @ts-ignore
var ofApp = Application("OmniFocus")

var script = `
    let id = ${JSON.stringify(args.id)};
    let name = ${JSON.stringify(args.name)};
    let note = ${JSON.stringify(args.note)};
    
    let t = Task.byIdentifier(id)
    if (t === null) {
        throw new Error("task not found: " + id)
    }
    
    t.name = name
    t.note = note
    
    JSON.stringify({
        id: t.id.primaryKey,
        name: t.name,
        note: t.note
    })
`

ofApp.evaluateJavascript(script);
