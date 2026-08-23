import { Controller } from "@hotwired/stimulus"

// A form with no submit button: picking a value is the whole interaction.
//
// Debounced because a native select fires `change` on every arrow key while
// it is closed. Without debouncing, a keyboard user going 5 → 1 triggers four
// saves. Each save causes a full-page redraw that lands mid-keystroke, and a
// refusal on any intermediate width strands a group. Waiting for the run to
// stop makes it one save. This also matches the grid's own keyboard model,
// where a carried row moves freely and commits once.
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
