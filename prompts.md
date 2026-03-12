
# 1

Using the code at https://github.com/mikerhodes/github-to-omnifocus as an example, plan
how you would use a go + JS approach to create a Kanban board application.

The tasks would be found using tags (eg, "kanban : inprogress", "kanban : backload"). So
you'd need to use Omnifocus JS (https://omni-automation.com/omnifocus/tutorial/index.html)
 to read the tasks.

The Go code should be a server that serves up plain HTML single page app. Use lightweight
JS -- NO react!

The Go code should render a column view with four columns - backlog, blocked, inprogress,
done - driven from the Omnifocus tags. Each task should be a card in the appropriate
column.

As in the github-to-omnifocus repo, there will need to be a translation from JS to Go
structs. Make sure to carry over the task IDs! Copy over the title from the tasks. Don't
worry about due dates and so on -- just the ID and title.

This is read-only for now. I just want to be able to visualise in the column view.

So the work is something like:

1. Build a Go web app with a column view for tasks.
2. Figure out how to read the tasks from Omnifocus by tag. Use constants for the tags for
now rather than config.
3. Integrate the Omnifocus tasks into the column view.

# 2

Add logging around the code that gets tasks from omnifocus. Add standard log line for
http requests
