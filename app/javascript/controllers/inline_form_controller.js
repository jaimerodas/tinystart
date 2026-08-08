import { Controller } from "@hotwired/stimulus"

// Connects to data-controller="inline-form"
//
// Swaps a trigger for the form it guards, in place: the "add" buttons at the
// bottom of a column or a group, and the rows that an edit button turns into a
// form. Both halves are already in the DOM, so opening one costs no round trip.
//
// The server can hand back a form that is already open — a failed save keeps
// its errors visible, and a successful "add link" leaves the form ready for the
// next one.
export default class extends Controller {
  static targets = ["trigger", "form", "field"]
  static values = { open: { type: Boolean, default: false } }

  connect() {
    this.render({ focus: this.openValue })
  }

  open(event) {
    event.preventDefault()
    this.openValue = true
    this.render({ focus: true })
  }

  close(event) {
    event?.preventDefault()
    this.openValue = false

    // An abandoned edit should leave nothing behind — not the values a failed
    // save had refused, and not the errors that came back with them. reset()
    // is no use here: it restores what was rendered, which after a rejection is
    // the bad input itself.
    this.formTarget.querySelectorAll("[data-pristine]").forEach(field => {
      field.value = field.dataset.pristine
    })
    this.formTarget.querySelector(".form-errors")?.remove()

    this.render({ focus: false })
    this.triggerButton()?.focus()
  }

  // The trigger is the button itself when adding, and the row holding the edit
  // button when editing.
  triggerButton() {
    const trigger = this.triggerTarget
    return trigger.matches("button") ? trigger : trigger.querySelector("button")
  }

  render({ focus }) {
    this.triggerTarget.hidden = this.openValue
    this.formTarget.hidden = !this.openValue

    if (focus && this.hasFieldTarget) this.fieldTarget.focus()
  }
}
