
const REFRESH_MS = 600000;
const THIRTY_DAYS_MS = 30 * 24 * 60 * 60 * 1000;
const NINETY_DAYS_MS = 90 * 24 * 60 * 60 * 1000;

document.addEventListener('alpine:init', () => {
  Alpine.store('status', { msg: "", error: "" });
  Alpine.data("kanban", () => ({
    board: { "backlog": [], "ready": [], "inprogress": [] },
    columns: {
      "backlog": "Backlog",
      "ready": "Ready",
      "inprogress": "In progress"
    },

    async init() {
      await this.loadBoard();
      // TODO add destroy method clears timer
      setInterval(() => this.loadBoard(false), REFRESH_MS);
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
        setStatus("Last updated: " + new Date().toLocaleTimeString());
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

    // addTask adds task name to a column
    addTask(col, name) {
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

      if (fromCol === toCol) return;

      // Remove from source column
      this.board[fromCol] = this.board[fromCol].filter(c => c.id !== card.id);

      // Add to destination column
      this.board[toCol] = [...(this.board[toCol] ?? []), card];

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
      card.done = e.target.checked;

      const clearCompletionTimer = () => {
        if (this.completionTimers[card.id]) {
          clearTimeout(this.completionTimers[card.id]);
          delete this.completionTimers[card.id];
        }
      };

      if (card.done) {
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
    /https?:\/\/[^\s<>"']*/g,
    function(url) {
      return '<a href="' + url + '" target="_blank" rel="noopener">' + url + '</a>';
    }
  );
}

function setStatus(msg) {
  Alpine.store("status").error = "";
  Alpine.store("status").msg = msg;
}

function setError(msg) {
  Alpine.store("status").msg = "";
  Alpine.store("status").error = msg;
}
