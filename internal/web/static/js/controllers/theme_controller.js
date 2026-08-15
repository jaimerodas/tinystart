import { Controller } from "@hotwired/stimulus"

// Connects to data-controller="theme"
export default class extends Controller {
  updateTheme(event) {
    // Only proceed if the form submission was successful
    if (event.detail.success) {
      // Find the checked radio button for theme_preference
      const selectedTheme = this.element.querySelector('input[name="user[theme_preference]"]:checked')
      const selectedColor = this.element.querySelector('input[name="user[color_preference]"]:checked')

      if (selectedTheme) {
        // Update the data-theme attribute on the html element
        document.documentElement.dataset.theme = selectedTheme.value
      }

      if (selectedColor) {
        // Update the data-color attribute on the html element
        document.documentElement.dataset.color = selectedColor.value
      }
    }
  }
}
