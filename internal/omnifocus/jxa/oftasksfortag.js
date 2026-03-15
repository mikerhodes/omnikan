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
// Uses evaluateJavascript() to run filtering in the OmniJS context, which avoids
// per-property JXA bridge crossings (~1ms each). tagsMatching() + tag.tasks is
// fast (~150ms) because it starts from a small, pre-filtered set.

ObjC.import('stdlib');
var args = JSON.parse($.getenv('OSA_ARGS'));

// @ts-ignore
var ofApp = Application("OmniFocus");

var script = `
    var tag = tagsMatching(${JSON.stringify(args.tag)})[0];
    var tasks = tag.tasks.filter(function(t) {
        if ([Task.Status.Completed, Task.Status.Dropped].includes(t.taskStatus)) {
            return false;
        }
        return t.containingProject && t.containingProject.id.primaryKey === ${JSON.stringify(args.projectId)};
    }).map(function(t) {
        return { id: t.id.primaryKey, name: t.name, note: t.note, added: t.added };
    });
    JSON.stringify(tasks);
`;

ofApp.evaluateJavascript(script);
