import { Controller } from "@hotwired/stimulus"

// Connects to data-controller="flash"
export default class extends Controller {
  static targets = ["message"]

  // The message comes with the page, so it is there without JavaScript too.
  // But a live region only speaks for changes made inside it after it is in
  // the accessibility tree, not for the text it arrived with. So the message
  // is taken out of the tree and put back a moment later. On screen nothing
  // changes.
  connect() {
    this.messageTarget.setAttribute("aria-hidden", "true")
    this.timeout = setTimeout(() => this.messageTarget.removeAttribute("aria-hidden"), 100)
  }

  disconnect() {
    clearTimeout(this.timeout)
  }

  dismiss() {
    this.element.remove()
  }
}
