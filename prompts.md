# What is this

This is the first few prompts I used to build out the bones of the application.

I stopped recording them at some point, but they are a useful thing to look back at.

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

# 3

Why does the code take so long to read the items, it's taking 20+ seconds per tag.

> Claude thought about this and suggested why, but said it wasn't sure on the syntax. It suggested a couple of alternatives.

Try both of them out. Write small JS scripts and run them to see which is faster. Use
the current approach as a baseline. Show me how they compare.

> Claude figured a few JS files and benchmarked.

Implement the fastest.

# 4

> I wanted to add and remove tags, but we were not sure how

Write a script that adds a task to the inbox, then tries to
add a tag. Pause to let me check the task exists. Then we can write a script to swap the
tag.

Use the existing "test" tag.

> Claude wrote a new script, tried creating a task with a tag, asked me, and wrote a script to change it. Then we updated the JXA notes file.

# Add moving tasks

Can you write a script to list the tags, I want to see what they look like. Output them
here.

> With this we could see the tags in play. Mutually-exclusive tags are a bit odd.

Okay. Let's write the code to allow us to move between the backlog, ready, and
inprogress columns/tags.

1. Update the Go code so that it's got a cache of the tasks in memory.
2. Add an endpoint to the Go app that accepts the task ID and the new column/tag.
3. The endpoint needs to move the item. Use the code we just used to swap out tags to
write a new JXA script that will swap the tag. The in-memory cache of the tasks will say
what the old tag was.
4. Add code to the HTML page to allow moving the items between columns.
5. Wire up the HTML code to the new backend to update the tags.

# Remove done

Let's remove the done column. I don't have a done state. That's just Omnifocus completed
 tasks.

# drag and drop

Please remove the buttons to move the cards and use drag and drop instead.

> Claude made the cards move

Make the cards move immediately and run the tag update call to the backend async

# look 'n' feel

Let's move the "loading" up to the top right rather than at the bottom.

Make the columns full page height, and make them scroll inside. That will make drag and
drop easier.

# complete

Let's now add a button to mark tasks "done". Make it look nice, like a checkbox that
fires an event. The item should grey out when checked but remain in place for 60s so the
user can uncheck the checkbox in case they made a mistake.

> Claude wrote several JXA scripts to test marking done

# focus in progress

❯ Make the in-progress column wider to highlight it. 25%, 25%, 50%.

❯ Also slightly fade the text in the non-in-progress columns.

❯ No, don't decrease the opacity. Just make the text itself slightly grey. Like #666 or
something.

❯ Fade the heading text and count blob a bit too

# Performance

❯ I don't like waiting for the tasks to load in the UX. Let's change that. Instead, have
the Go app read the items at startup, then refresh every 10 minutes. Keep the items in a
cache and change the callback to load the items so it uses the cache. It shouldn't kick
off a new read from Omnifocus.


❯ Presumably the cached versions are updated when tasks are moved columns or marked
complete? So if the user refreshes the page, things don't appear wrong?

> Turned out this needed fixing

# notes are useful for inprogress

❯ Add the Omnifocus notes to the In Progress tasks only. Make sure the cards still look
nice. The notes text should be smaller than the title.

❯ Make sure that URLs in the notes are clickable.

❯ That regex isn't working. For https://dx13.co.uk/foo it's only linking https://dx13.c

# UX updates

A bunch of prompts for tweaking UX.

Warm up the colours in the UX.

Add a refresh button.

Make the buttons look all buttony. Apply a similar style to the notes.

Add the hotkey for adding Omnifocus tasks.
