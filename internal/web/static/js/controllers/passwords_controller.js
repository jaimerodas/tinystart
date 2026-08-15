import { Controller } from "@hotwired/stimulus"

// Connects to data-controller="passwords"
export default class extends Controller {
  static targets = [ "password", "actions", "tokenP", "notice" ]

  connect() {
    this.addToggleButton()
  }

  addToggleButton() {
    const button = document.createElement("button")
    button.textContent = "Toggle Password Visibility"
    button.setAttribute("data-action", "click->passwords#togglePasswordVisibility")
    this.actionsTarget.appendChild(button)
  }

  togglePasswordVisibility() {
    this.passwordTargets.forEach((pwd) => {
      if (pwd.type === "password") { pwd.type = "text" }
      else { pwd.type = "password" }
    })
  }

  copyToken(e) {
    var key = e.target.textContent
    navigator.clipboard.writeText(key)
    this.showCopiedDialog()
    console.log("Copied API Key to clipboard!")
  }

  showCopiedDialog() {
    if (this.hasNoticeTarget) { this.noticeTarget.remove() }
    const text = document.createElement("span")
    text.setAttribute("data-passwords-target", "notice")
    text.textContent = " (Copied!)"
    this.tokenPTarget.appendChild(text)

    setTimeout(()=> { if (this.hasNoticeTarget) { this.noticeTarget.remove() } }, 1000)
  }
}
