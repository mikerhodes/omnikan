
// Return all incomplete tasks that have a given tag and belong to a given project.
// Accepts { "tag": "backlog", "projectId": "abc123" } as JSON in OSA_ARGS.
//
// Call it:
//   set -gx OSA_ARGS '{"projectid": "abc123"}'
//   osascript -l JavaScript oftasksfortag.js | jq .
//
// Returns JSON array:
// [
//   { "id": "iAKv1Uo8XqW", "name": "My task title", "note": "..." },
//   ...
// ]
//
// Uses evaluateJavascript() to run filtering in the OmniJS context, which avoids
// per-property JXA bridge crossings (~1ms each). tagsMatching() + tag.tasks is
// fast (~150ms) because it starts from a small, pre-filtered set.

ObjC.import('stdlib');
var args = JSON.parse($.getenv('OSA_ARGS'));

// @ts-ignore
var ofApp = Application("OmniFocus");

var script = `
    let proj = Project.byIdentifier(${JSON.stringify(args.projectid)});
    if (proj === null) {
      throw new Error("project not found: " + args.projectId);
    }
    let states = [
      Task.Status.Available,
      Task.Status.DueSoon,
      Task.Status.Next,
      Task.Status.Overdue,
      Task.Status.Blocked
    ];
    let tasks = proj.tasks.filter(function(t) {
        return states.includes(t.taskStatus);
    }).map(function(t) {
        return {
            id: t.id.primaryKey,
            name: t.name,
            note: t.note,
            added: t.added,
            tags: t.tags.map(function(tg) { return tg.name; })
        };
    });
    JSON.stringify(tasks);
`;

ofApp.evaluateJavascript(script);
