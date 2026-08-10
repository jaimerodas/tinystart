import { Controller } from "@hotwired/stimulus"

// Connects to data-controller="start-shortcuts" on <main class="start-page">.
//
// The chords that move between the two states of the start page, plus the ?
// that lists every key either of them answers to. grid_keyboard drives the
// editor's grid; this drives the pages.
//
// Only the value a page can act on is set — show gets edit, edit gets view — so
// "does nothing on the page it would take you to" comes out of the markup
// rather than out of a branch in here.
export default class extends Controller {
  static values = { edit: String, view: String }
  static targets = ["dialog"]

  keydown(event) {
    if (this.isChord(event, "KeyE")) return this.leaveFor(event, this.editValue)
    if (this.isChord(event, "KeyS")) return this.leaveFor(event, this.viewValue)
    if (this.isHelp(event)) return this.openDialog(event)
  }

  // event.code, not event.key: on a Mac ⌥E is a dead key and ⌥S is ß, so the
  // key a chord produces says nothing about the key that was pressed.
  isChord(event, code) {
    return event.altKey && !event.metaKey && !event.ctrlKey && event.code === code
  }

  // The command bar is autofocused, so ? has to be typeable there — it is only
  // a shortcut when nothing is being typed into.
  isHelp(event) {
    if (event.key !== "?" || event.altKey || event.metaKey || event.ctrlKey) return false

    return !event.target.closest("input, textarea, select, [contenteditable]")
  }

  // Swallowed on both pages, including the one where there is nowhere to go:
  // focus is usually in the command bar, and on a Mac these chords stand for ´
  // and ß. A shortcut a page cannot act on must still not type into it.
  leaveFor(event, url) {
    event.preventDefault()
    if (!url) return

    // Leaving is leaving, whichever way you do it. A row still being carried in
    // the editor has to be dropped and saved before the page goes, the same
    // rule Tab and clicking away already follow. grid_keyboard listens for this
    // rather than being called, so the two stay strangers.
    //
    // It answers with the save it started. Waiting on that is the whole point:
    // firing the POST first is not the same as it being processed first, and
    // the page being visited reads the same table. Landing before the move
    // commits renders the order it was about to replace.
    const leaving = new CustomEvent("start-page:leaving", { detail: { waitFor: [] } })
    window.dispatchEvent(leaving)

    Promise.all(leaving.detail.waitFor).then(() => Turbo.visit(url))
  }

  openDialog(event) {
    if (!this.hasDialogTarget || this.dialogTarget.open) return

    event.preventDefault()
    this.dialogTarget.showModal()
  }

  // showModal() sets the open attribute, and Turbo snapshots the page with it
  // still there. Restoring that snapshot — Back after a chord — renders the
  // panel inline instead of in the top layer: no backdrop, no focus trap, and
  // Esc no longer reaches it, while openDialog refuses to reopen something it
  // already believes is open. Close it before it can be photographed.
  closeDialog() {
    if (this.hasDialogTarget && this.dialogTarget.open) this.dialogTarget.close()
  }

  // A modal <dialog> fills the viewport with its own box, so a click anywhere
  // outside the panel still lands on the dialog itself. That is the backdrop.
  closeOnBackdrop(event) {
    if (event.target === this.dialogTarget) this.dialogTarget.close()
  }
}
