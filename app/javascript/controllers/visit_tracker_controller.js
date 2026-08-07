import { Controller } from "@hotwired/stimulus"
import { trackTileVisit } from "lib/track_visit"

// Counts a visit when a tile is opened.
//
// Attached once to the grid; clicks are handled by delegation so it covers
// every a[data-item-id] inside, including tiles rendered later by a Turbo
// Stream. The href is never touched, so opening in a new tab still works.
export default class extends Controller {
  connect() {
    this.onClick = this.onClick.bind(this)
    // auxclick fires for middle-click; click covers left + cmd/ctrl-click.
    this.element.addEventListener("click", this.onClick)
    this.element.addEventListener("auxclick", this.onClick)
  }

  disconnect() {
    this.element.removeEventListener("click", this.onClick)
    this.element.removeEventListener("auxclick", this.onClick)
  }

  onClick(event) {
    // Only count opens: left-click (0) or middle-click (1). Ignore right-click.
    if (event.button !== 0 && event.button !== 1) return

    const tile = event.target.closest("a[data-item-id]")
    if (!tile || !this.element.contains(tile)) return

    trackTileVisit(tile.dataset.itemId)
  }
}
