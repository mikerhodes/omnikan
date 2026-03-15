// Return the ID of a project by name.
// Accepts { "projectName": "My Project" } as JSON in OSA_ARGS.
// Returns JSON: { "id": "abc123" }
// Exits with a non-zero status if the project is not found.

ObjC.import('stdlib')
var args = JSON.parse($.getenv('OSA_ARGS'))

// @ts-ignore
var ofApp = Application("OmniFocus")
var ofDoc = ofApp.defaultDocument

var matches = ofDoc.flattenedProjects.whose({ name: args.projectName })
if (matches.length === 0) {
    throw new Error("project not found: " + args.projectName)
}

JSON.stringify({ id: matches()[0].id() })
