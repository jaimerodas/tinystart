import { Controller } from "@hotwired/stimulus"

// Connects to data-controller="theme"
export default class extends Controller {
  // The page shows a pick as it is made. The save follows it (auto-submit),
  // and the page links every typeface it offers, so a new font has its
  // stylesheet already. A refused save says so in #preferences_status.
  update() {
    const fields = this.element.elements
    const html = document.documentElement.dataset
    html.theme = fields["user[theme_preference]"].value
    html.color = fields["user[color_preference]"].value
    html.font = fields["user[font_preference]"].value
  }
}
