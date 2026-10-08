import { Controller } from "@hotwired/stimulus"

// Connects to data-controller="theme"
export default class extends Controller {
  updateTheme(event) {
    // If the form submission succeeded, proceed.
    if (event.detail.success) {
      // Find the checked radio button for theme_preference
      const selectedTheme = this.element.querySelector('input[name="user[theme_preference]"]:checked')
      const selectedColor = this.element.querySelector('input[name="user[color_preference]"]:checked')
      const selectedFont = this.element.querySelector('input[name="user[font_preference]"]:checked')

      if (selectedTheme) {
        // Update the data-theme attribute on the html element
        document.documentElement.dataset.theme = selectedTheme.value
      }

      if (selectedColor) {
        // Update the data-color attribute on the html element
        document.documentElement.dataset.color = selectedColor.value
      }

      if (selectedFont) {
        // The page the save redirects to links the new font's stylesheet,
        // and Turbo adds it to <head>. This switches the page over to it.
        document.documentElement.dataset.font = selectedFont.value
      }
    }
  }
}
