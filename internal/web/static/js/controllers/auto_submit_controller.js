import { Controller } from "@hotwired/stimulus"

// A form with no submit button: picking a value is the whole interaction.
//
// Debounced because a native select fires `change` on every arrow key while
// it is closed. Submitting each one would send a keyboard user going 5 → 1
// through four saves — four full-page redraws landing mid-keystroke, and a
// refusal for every intermediate width that would strand a group. Waiting for
// the run to stop makes it one save, which is also what the grid's own
// keyboard model does: a carried row moves freely and commits once.
export default class extends Controller {
  static values = { delay: { type: Number, default: 300 } }

  submit() {
    clearTimeout(this.timeout)
    this.timeout = setTimeout(() => this.element.requestSubmit(), this.delayValue)
  }

  disconnect() {
    clearTimeout(this.timeout)
  }
}
