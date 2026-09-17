
const REFRESH_MS = 600000;
const STATUS_REFRESH_MS = 5000;
const THIRTY_DAYS_MS = 30 * 24 * 60 * 60 * 1000;
const NINETY_DAYS_MS = 90 * 24 * 60 * 60 * 1000;
const COLUMN_KEYS = { "1": "backlog", "2": "ready", "3": "inprogress" };
const HTTP_URL_PATTERN = /https?:\/\/[^\s<>"']*/;

const SHORTCUTS = [
  {
    binding: "j",
    description: "Select next task",
    run: board => board.selectAdjacent(1),
  },
  {
    binding: "k",
    description: "Select previous task",
    run: board => board.selectAdjacent(-1),
  },
  {
    binding: "([1-3])",
    description: "Select a column",
    run: (board, event) => board.selectColumn(COLUMN_KEYS[event.key]),
  },
  {
    binding: "h",
    description: "Move task left",
    run: board => board.moveSelected(-1),
  },
  {
    binding: "l",
    description: "Move task right",
    run: board => board.moveSelected(1),
  },
  {
    binding: "a",
    description: "Add a task",
    run: board => board.focusAddTask(),
  },
  {
    binding: "e",
    description: "Edit selected task",
    run: board => board.editSelected(),
  },
  {
    binding: "o",
    description: "Open selected task URL",
    run: board => board.openSelectedURL(),
  },
  {
    binding: "x",
    description: "Complete or undo completion",
    run: board => board.toggleSelectedCompletion(),
  },
  {
    binding: "d d",
    description: "Delete selected task",
    run: board => board.deleteSelected(),
  },
  {
    binding: "r",
    description: "Refresh from OmniFocus",
    run: board => board.loadBoard(true),
  },
  {
    binding: "[Shift]+?",
    description: "Show keyboard shortcuts",
    run: board => { board.showShortcuts = !board.showShortcuts; },
  },
  {
    binding: "Escape",
    description: "Cancel or clear selection",
    run: board => board.clearKeyboardState(),
  },
  {
    binding: "$mod+Enter",
    description: "Save an edit",
  },
];

document.addEventListener('alpine:init', () => {
  Alpine.store('status', { msg: "", error: "" });
  Alpine.data("kanban", () => ({
    board: { "backlog": [], "ready": [], "inprogress": [] },
    selectedColumn: "backlog",
    selectedTaskID: null,
    showShortcuts: false,
    shortcuts: SHORTCUTS,
    removeKeyboardBindings: null,
    refreshTimer: null,
    statusTimer: null,
    initStatus: null,
    columns: {
      "backlog": "Backlog",
      "ready": "Ready",
      "inprogress": "In progress"
    },

    async init() {
      const bindings = Object.fromEntries(
        SHORTCUTS
          .filter(shortcut => shortcut.run)
          .map(shortcut => [
            shortcut.binding,
            event => {
              event.preventDefault();
              shortcut.run(this, event);
            },
          ])
      );
      this.removeKeyboardBindings = window.tinykeys.tinykeys(
        window,
        bindings,
        { timeout: 2000 }
      );
      await this.loadBoard();
      this.refreshTimer = setInterval(() => this.loadBoard(false), REFRESH_MS);
      this.statusTimer = setInterval(() => this.refreshStatus(), STATUS_REFRESH_MS);
    },

    destroy() {
      this.removeKeyboardBindings?.();
      clearInterval(this.refreshTimer);
      clearInterval(this.statusTimer);
    },

    async loadStatus() {
      const response = await fetch("/api/status");
      if (!response.ok) {
        throw new Error(`Response status: ${response.status}`);
      }
      const status = await response.json();
      this.initStatus = status;

      if (status.state === "ready") {
        return true;
      }
      if (status.state === "configuration_error") {
        setError("Configuration error: " + status.error);
      } else if (status.state === "retrying") {
        const retry = status.nextRetry
          ? " Retrying at " + new Date(status.nextRetry).toLocaleTimeString() + "."
          : " Retrying.";
        setError("OmniFocus initialization failed." + retry + " " + status.error);
      } else if (status.state === "degraded") {
        setError("OmniFocus refresh failed: " + status.error);
      } else {
        setStatus("Initializing OmniFocus…");
      }
      return false;
    },

    async refreshStatus() {
      const wasReady = this.initStatus?.state === "ready";
      try {
        const ready = await this.loadStatus();
        if (ready && !wasReady) {
          await this.loadBoard(false);
        }
      } catch (error) {
        console.error(error.message);
        setError("Failed to load status: " + error.message);
      }
    },

    async loadBoard(reloadFromOmnifocus) {
      setStatus("Loading board…");
      const url = reloadFromOmnifocus ? "/api/board?force=true" : "/api/board";
      try {
        const response = await fetch(url);
        if (!response.ok) {
          throw new Error(`Response status: ${response.status}`);
        }
        const result = await response.json();
        for (const col of Object.keys(this.columns)) {
          for (const card of result[col] ?? []) {
            card.editing = false;
            card.done = false;
          }
        }
        this.board = result;
        this.ensureSelection();
        if (await this.loadStatus()) {
          setStatus("Last updated: " + new Date().toLocaleTimeString());
        }
      } catch (error) {
        console.error(error.message);
        setError("Failed to load board: " + error.message);
      }
    },

    // cardsWithSectionDividers inserts headers into the
    // cards column for 30 days old etc.
    cardsWithSectionDividers(col) {
      const cutoff30 = new Date(Date.now() - THIRTY_DAYS_MS);
      const cutoff90 = new Date(Date.now() - NINETY_DAYS_MS);
      const cards = this.board[col];
      let display = [];

      let currentSection = "";
      for (const c of cards) {
        const added = c.added && new Date(c.added);
        const section = added > cutoff30 ? null
          : added > cutoff90 ? "Older than 30 days"
            : "Older than 90 days";
        if (section != currentSection) {
          currentSection = section;
          display.push({ _divider: true, label: section })
        }

        display.push(c);
      }

      return display;
    },

    ensureSelection() {
      if (!this.selectedTaskID) return;
      const selectedStillExists = Object.values(this.board)
        .some(cards => cards.some(card => card.id === this.selectedTaskID));
      if (!selectedStillExists) {
        this.selectedTaskID = this.board[this.selectedColumn]?.[0]?.id ?? null;
      }
    },

    selectedCard() {
      return this.board[this.selectedColumn]
        ?.find(card => card.id === this.selectedTaskID);
    },

    selectCard(col, card) {
      this.selectedColumn = col;
      this.selectedTaskID = card.id;
    },

    scrollSelectionIntoView() {
      Alpine.nextTick(() => {
        document.querySelector("[data-selected='true']")
          ?.scrollIntoView({ block: "nearest" });
      });
    },

    selectColumn(col) {
      this.selectedColumn = col;
      this.selectedTaskID = this.board[col]?.[0]?.id ?? null;
      this.scrollSelectionIntoView();
    },

    selectAdjacent(offset) {
      const cards = this.board[this.selectedColumn] ?? [];
      if (cards.length === 0) {
        this.selectedTaskID = null;
        return;
      }

      const current = cards.findIndex(card => card.id === this.selectedTaskID);
      const next = current === -1
        ? (offset > 0 ? 0 : cards.length - 1)
        : Math.max(0, Math.min(cards.length - 1, current + offset));
      this.selectedTaskID = cards[next].id;
      this.scrollSelectionIntoView();
    },

    focusAddTask() {
      document.getElementById(`add-input-${this.selectedColumn}`)?.focus();
    },

    editSelected() {
      const card = this.selectedCard();
      if (card) this.startEdit(card);
    },

    openSelectedURL() {
      const card = this.selectedCard();
      if (!card) return;

      const url = firstURL(card.name) ?? firstURL(card.note);
      if (url) window.open(url, "_blank", "noopener,noreferrer");
    },

    toggleSelectedCompletion() {
      const card = this.selectedCard();
      if (card) this.setTaskCompletion(card, !card.done);
    },

    deleteSelected() {
      const card = this.selectedCard();
      if (card) this.deleteTask(card);
    },

    clearKeyboardState() {
      this.showShortcuts = false;
      this.selectedTaskID = null;
    },

    moveSelected(offset) {
      const card = this.selectedCard();
      if (!card) return;
      const columns = Object.keys(this.columns);
      const current = columns.indexOf(this.selectedColumn);
      const target = columns[current + offset];
      if (target) this.moveTask(card, this.selectedColumn, target);
    },

    // addTask adds task name to a column
    addTask(col, name) {
      if (!name.trim()) return;
      setStatus("Adding item…");
      fetch("/api/add", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: name, col: col })
      })
        .then((r) => {
          if (!r.ok) throw new Error("HTTP " + r.status);
          return r.json();
        })
        .then((task) => {
          this.board[col].unshift({
            id: task.id,
            name: task.name,
            note: task.note,
            added: new Date(),
            done: false,
            editing: false,
          });
          this.selectedColumn = col;
          this.selectedTaskID = task.id;
          setStatus("Last updated: " + new Date().toLocaleTimeString());
        })
        .catch((err) => {
          setError("Failed to add task: " + err.message);
        });
    },

    removeCard(card) {
      for (const [col, cards] of Object.entries(this.board)) {
        this.board[col] = cards.filter(c => c.id !== card.id);
      }
      this.ensureSelection();
    },

    deleteTask(card) {
      this.removeCard(card);
      fetch("/api/delete", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ id: card.id })
      })
        .then((r) => {
          if (!r.ok) throw new Error("HTTP " + r.status);
        })
        .catch((err) => {
          console.error(err.message);
          setError("Failed to delete task: " + err.message);
          this.loadBoard(false);
        });
    },

    startEdit(card) {
      card.draft = { name: card.name, note: card.note };
      card.editing = true;
      Alpine.nextTick(() => {
        document.querySelector("[data-editing='true'] input")?.focus();
      });
    },

    cancelEdit(card) {
      card.draft = {};
      card.editing = false;
    },

    saveEdit(card) {
      card.name = card.draft.name
      card.note = card.draft.note
      card.draft = {};
      card.editing = false;

      fetch("/api/edit", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ id: card.id, name: card.name, note: card.note })
      })
        .then((r) => {
          if (!r.ok) throw new Error("HTTP " + r.status);
        })
        .catch((err) => {
          setError("Failed to edit task: " + err.message);
          this.loadBoard(false);
        });
    },

    onDragStart(event, card, fromCol) {
      this.dragging = { card, fromCol };
      event.dataTransfer.effectAllowed = 'move';
    },

    onDrop(event, toCol) {
      if (!this.dragging) return;
      const { card, fromCol } = this.dragging;
      this.dragging = null;

      this.moveTask(card, fromCol, toCol);
    },

    moveTask(card, fromCol, toCol) {
      if (fromCol === toCol) return;
      this.board[fromCol] = this.board[fromCol].filter(c => c.id !== card.id);
      this.board[toCol] = [...(this.board[toCol] ?? []), card];
      this.selectedColumn = toCol;
      this.selectedTaskID = card.id;
      this.scrollSelectionIntoView();

      fetch("/api/move", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ id: card.id, newCol: toCol })
      })
        .then((r) => {
          if (!r.ok) throw new Error("HTTP " + r.status);
        })
        .catch((err) => {
          setError("Failed to move task: " + err.message);
          this.loadBoard(false);
        });
    },

    completionTimers: {},

    onCardCheckChange(e, card) {
      this.setTaskCompletion(card, e.target.checked);
    },

    setTaskCompletion(card, done) {
      card.done = done;

      const clearCompletionTimer = () => {
        if (this.completionTimers[card.id]) {
          clearTimeout(this.completionTimers[card.id]);
          delete this.completionTimers[card.id];
        }
      };

      if (done) {
        // After 60s, remove from board
        this.completionTimers[card.id] = setTimeout(() => {
          delete this.completionTimers[card.id];
          this.removeCard(card);
        }, 60000);

        fetch("/api/complete", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ id: card.id })
        })
          .then((r) => {
            if (!r.ok) throw new Error("HTTP " + r.status);
          })
          .catch((err) => {
            setError("Failed to complete task: " + err.message);
            clearCompletionTimer();
            this.loadBoard(false);
          });
      } else {
        // Undo: cancel the removal timer and mark incomplete
        clearCompletionTimer();

        fetch("/api/incomplete", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ id: card.id })
        })
          .then((r) => {
            if (!r.ok) throw new Error("HTTP " + r.status);
          })
          .catch((err) => {
            setError("Failed to undo completion: " + err.message);
            this.loadBoard(false);
          });
      }
    },
  }));
});

function escapeHtml(str) {
  return str
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

// Linkify URLs in already-escaped HTML. Matches http(s):// URLs and
// wraps them in an anchor tag. Must run after escapeHtml.
function linkify(escaped) {
  return escaped.replace(
    new RegExp(HTTP_URL_PATTERN.source, "g"),
    function(url) {
      return '<a href="' + url + '" target="_blank" rel="noopener">' + url + '</a>';
    }
  );
}

function firstURL(text) {
  return text?.match(HTTP_URL_PATTERN)?.[0] ?? null;
}

function setStatus(msg) {
  Alpine.store("status").error = "";
  Alpine.store("status").msg = msg;
}

function setError(msg) {
  Alpine.store("status").msg = "";
  Alpine.store("status").error = msg;
}
