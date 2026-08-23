import { Controller } from "@hotwired/stimulus"

// Waits for the user to approve a connected app's device authorization in
// another tab.
//
// The server holds the pending grant in the session. This controller only
// asks where it got to. On success the page reloads, so the server re-renders
// the whole section instead of this controller patching it together.
export default class extends Controller {
  static targets = ["status"]
  static values = { url: String, interval: { type: Number, default: 5000 } }

  connect() {
    this.check()
    this.timer = setInterval(() => this.check(), this.intervalValue)
  }

  disconnect() {
    clearInterval(this.timer)
  }

  async check() {
    try {
      const response = await fetch(this.urlValue, {
        headers: { Accept: "application/json" }
      })
      if (!response.ok) return

      const { status } = await response.json()

      if (status === "connected") {
        this.stopAnd("Connected. Reloading…")
      } else if (status === "denied") {
        this.stopAnd("Approval was denied. Reloading…")
      } else if (status === "expired" || status === "idle") {
        this.stopAnd("The request expired. Reloading…")
      }
    } catch {
      // A blip while waiting is fine. The next tick tries again.
    }
  }

  stopAnd(message) {
    clearInterval(this.timer)
    if (this.hasStatusTarget) this.statusTarget.textContent = message
    window.location.reload()
  }
}
