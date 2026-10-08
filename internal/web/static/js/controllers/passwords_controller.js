import { Controller } from "@hotwired/stimulus"

// Connects to data-controller="passwords"
export default class extends Controller {
  static targets = ["password", "toggle"]

  connect() {
    this.toggleTarget.hidden = false
  }

  toggle(event) {
    const type = event.target.checked ? "text" : "password"
    this.passwordTargets.forEach(field => field.type = type)
  }
}
