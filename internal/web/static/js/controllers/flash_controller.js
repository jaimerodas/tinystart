import { Controller } from "@hotwired/stimulus"

// Connects to data-controller="flash"
export default class extends Controller {
  static values = { duration: { type: Number, default: 4000 } }

  connect() {
    this.dismissing = false
    this.timeout = setTimeout(() => this.dismiss(), this.durationValue)
  }

  disconnect() {
    clearTimeout(this.timeout)
  }

  dismiss() {
    if (this.dismissing) return
    this.dismissing = true
    clearTimeout(this.timeout)
    this.element.classList.add("dismissing")
    this.element.addEventListener("animationend", () => this.element.remove(), { once: true })
  }
}
