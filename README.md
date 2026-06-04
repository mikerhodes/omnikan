# Omnikan

Omnikan is a Kanban board view for your [Omnifocus](https://www.omnigroup.com/omnifocus/) tasks.

![](./omnikan.png)

## Set up

1. Create a [mutually-exclusive set of tags](https://learnomnifocus.com/tutorial/simplify-tagging-with-mutually-exclusive-tags-in-omnifocus/):

    - `kanban:backlog`
    - `kanban:ready`
    - `kanban:inprogress`

2. Create a project for your kanban board; Omnikan displays tasks from a single project.

3. Ensure Omnifocus is running

4. Start Omnikan:

    ```
    go run . -project "my project"
    ```

5. Open http://localhost:8080 (by default).

## Background

Omnikan uses a running Omnifocus as its data backend. It uses Omnifocus JavaScript automation to communicate with the backend --- generating the board, moving tasks between columns (by updating Omnifocus tags) and completing tasks.

Why use OmniFocus as the backend? Because that gives me a powerful iOS application to manage tasks without my having to write one.
