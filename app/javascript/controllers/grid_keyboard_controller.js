import { Controller } from "@hotwired/stimulus"
import { moveGroup, moveItem, clearNotice } from "lib/start_page_moves"

// Connects to data-controller="grid-keyboard" on #start_page_grid.
//
// Makes the whole editor one Tab stop with a highlight the arrow keys walk, and
// lets a tile or a group be picked up with Space and carried a position at a
// time. Dragging is the pointer's way to reorder; this is the keyboard's.
// Neither knows about the other beyond sharing lib/start_page_moves.
//
// The rows are `.item-row`, `.group-header` and the "Add link" / "Add group"
// triggers — every line of the editor you can act on. They render with
// tabindex="-1" and exactly one is promoted to 0, so Tab enters the grid once
// and leaves it once however many tiles are on the page.
//
// Nothing here reimplements adding, editing or deleting: Enter and Delete find
// the button that already does the job and click it, which keeps the inline
// forms, the confirm dialogs and their focus handling in one place.
export default class extends Controller {
  static targets = ["row"]

  // In initialize rather than connect because Stimulus fires the target
  // callbacks for the initial DOM around connect, and a row that arrives first
  // must not have its adoption wiped by a later reset.
  initialize() {
    this.grabbed = null
    this.grabOrigin = null
    this.pendingFocusKey = null
    this.currentRow = null
    this.lastIndex = 0
    this.refocusAfterDelete = false
  }

  // === THE ROVING TAB STOP ===
  //
  // Rows arrive and leave whenever a Turbo Stream replaces a column or a group,
  // which is every write on this page. Reacting to the targets themselves means
  // the tab stop never has to be rebuilt on a timer or re-queried after a
  // render.

  rowTargetConnected(row) {
    row.tabIndex = -1

    if (this.pendingFocusKey && this.keyFor(row) === this.pendingFocusKey) {
      this.pendingFocusKey = null
      this.focusRow(row)
    } else if (!this.currentRow?.isConnected) {
      // Keep a way into the grid at all times, but don't pull focus — this runs
      // on every render, including ones nobody is looking at.
      this.currentRow = row
      row.tabIndex = 0
    }
  }

  rowTargetDisconnected(row) {
    if (row !== this.currentRow) return

    this.currentRow = null
    // The replacement rows have not arrived yet; let the render finish first.
    queueMicrotask(() => this.claimRowAfterDelete())
  }

  // A deleted row leaves the highlight nowhere, and dumping focus on <body>
  // sends a keyboard user back to the top of the document. Take the place the
  // deleted row occupied instead.
  claimRowAfterDelete() {
    if (!this.refocusAfterDelete) return

    this.refocusAfterDelete = false
    if (this.rows.length === 0) return

    this.focusRow(this.rows[Math.min(this.lastIndex, this.rows.length - 1)])
  }

  // === KEYBOARD MODE ===
  //
  // None of the keys below do anything until focus is in the grid, so the page
  // says which state it is in: the legend swaps for the key list, and the drag
  // handles withdraw rather than offering a second way to move a row that may
  // already be carried.

  enter(event) {
    this.syncKeyboardMode()
    this.adopt(event)
  }

  leave() {
    // Reparenting a carried row drops focus to <body> for an instant, and the
    // restoration happens in the same task. Decide on the next one.
    queueMicrotask(() => {
      // Focus left the grid entirely while something was still being carried —
      // a click on the page outside it. Letting go commits, the same rule Tab
      // follows, rather than leaving a move dangling to land later.
      if (this.grabbed && !this.element.contains(document.activeElement)) {
        this.drop({ refocus: false })
      }

      this.syncKeyboardMode()
    })
  }

  syncKeyboardMode() {
    // A move or a delete destroys the focused row, and focus sits on <body>
    // until the render brings it back. That is a round trip, not a departure —
    // reading it as one flickers the handles back and swaps the legend twice.
    if (this.pendingFocusKey || this.refocusAfterDelete) return

    if (!this.element.contains(document.activeElement)) {
      this.element.classList.remove("keyboard-mode")
      return
    }

    // :focus-visible is the browser's own answer to "did they mean to do this
    // with the keyboard" — a click on a row focuses it but does not match, so a
    // pointer user never loses the handles for reaching one.
    //
    // Entering is one-way until focus leaves: a programmatic focus() after a
    // move need not match, and the handles must not flicker back mid-carry.
    if (document.activeElement.matches(":focus-visible")) {
      this.element.classList.add("keyboard-mode")
    }
  }

  // Focus can land on a row without us putting it there: closing an inline form
  // returns focus to its trigger, and a pointer user can just click one. Adopt
  // whatever got focused so the highlight never disagrees with the caret.
  adopt(event) {
    const row = event.target.closest('[data-grid-keyboard-target="row"]')
    if (!row) return

    // Focus left the row being carried — a click elsewhere in the grid. Letting
    // go commits, the same as dragging.
    if (this.grabbed && !this.grabbed.contains(row)) this.drop({ refocus: false })

    if (row !== this.currentRow) this.setCurrentRow(row)
  }

  // Document order, which for this grid means column by column and within a
  // column top to bottom — the order the eye reads it in.
  //
  // A row whose inline form is open is hidden, and navigating onto something
  // invisible would strand the highlight, so those drop out of the list.
  get rows() {
    return this.rowTargets.filter(row => row.offsetParent !== null)
  }

  setCurrentRow(row) {
    if (this.currentRow) this.currentRow.tabIndex = -1
    this.currentRow = row
    row.tabIndex = 0
    this.lastIndex = this.rows.indexOf(row)
  }

  focusRow(row) {
    this.setCurrentRow(row)
    row.focus()
  }

  // A row's key is the id of the node it belongs to — item_12, group_4,
  // new_item_group_4 — which StartPageHelper already builds for the Turbo Stream
  // targets. Nothing extra has to be rendered to identify a row.
  keyFor(row) {
    return row?.closest("[id]")?.id
  }

  rowIn(node) {
    return node.querySelector('[data-grid-keyboard-target="row"]')
  }

  // === NAVIGATION ===

  keydown(event) {
    if (event.metaKey || event.ctrlKey || event.altKey) return

    const row = event.target.closest('[data-grid-keyboard-target="row"]')
    if (!row) return

    // Once an inline form is open the keys belong to the form, not the grid.
    if (event.target.closest(".inline-form")) return

    const handled = this.grabbed
      ? this.handleGrabbedKey(event)
      : this.handleRowKey(event, row)

    if (handled) {
      event.preventDefault()
      event.stopPropagation()
    }
  }

  handleRowKey(event, row) {
    switch (event.key) {
      case "ArrowDown": return this.step(row, 1)
      case "ArrowUp": return this.step(row, -1)
      case "ArrowRight": return this.stepColumn(row, 1)
      case "ArrowLeft": return this.stepColumn(row, -1)
      case "Home": return this.jump(row, 0)
      case "End": return this.jump(row, -1)
      case "Enter": return this.activate(row)
      case "Delete":
      case "Backspace": return this.remove(row)
      case " ": return this.grab(row)
      default: return false
    }
  }

  // Returning true at an end of the list is deliberate: the key was meant for
  // the grid and swallowing it stops the page scrolling underneath a highlight
  // that did not move.
  step(row, delta) {
    const siblings = this.rowsInColumn(this.columnOf(row))
    const next = siblings[siblings.indexOf(row) + delta]
    if (next) this.focusRow(next)
    return true
  }

  jump(row, index) {
    const siblings = this.rowsInColumn(this.columnOf(row))
    this.focusRow(index === -1 ? siblings[siblings.length - 1] : siblings[index])
    return true
  }

  // Sideways is answered geometrically rather than by index: the row you want in
  // the next column is the one beside the one you are on, and two columns rarely
  // hold the same number of rows.
  stepColumn(row, delta) {
    const target = this.columnBeside(this.columnOf(row), delta)
    if (!target) return true

    const candidate = this.nearestTo(this.rowsInColumn(target), this.midpointOf(row))
    if (candidate) this.focusRow(candidate)
    return true
  }

  columnOf(element) {
    return element.closest(".start-page-column")
  }

  get columns() {
    return [ ...this.element.querySelectorAll(".start-page-column") ]
  }

  columnBeside(column, delta) {
    return this.columns[this.columns.indexOf(column) + delta]
  }

  rowsInColumn(column) {
    return this.rows.filter(row => this.columnOf(row) === column)
  }

  midpointOf(element) {
    const rect = element.getBoundingClientRect()
    return rect.top + rect.height / 2
  }

  nearestTo(elements, y) {
    return elements.reduce((closest, element) => {
      if (!closest) return element
      return Math.abs(this.midpointOf(element) - y) < Math.abs(this.midpointOf(closest) - y)
        ? element
        : closest
    }, null)
  }

  // === ACTIONS ===
  //
  // Both click a button that is already in the row, so the inline form's focus
  // handling and the delete confirm keep working exactly as they do for a
  // pointer.

  activate(row) {
    if (row.matches(".add-trigger")) {
      row.click()
    } else {
      row.querySelector(".edit-button")?.click()
    }
    return true
  }

  // The confirm can be declined, and then nothing is submitted and there is no
  // render to take the highlight back afterwards. Arming on the click would
  // leave that promise outstanding, for the next unrelated render to redeem by
  // pulling focus out of whatever the user had opened by then — so arm only
  // once the request is actually on its way. Aborting first drops the arming
  // left by any confirm that was declined earlier.
  remove(row) {
    const button = row.querySelector(".remove-button")
    if (!button) return true

    const index = this.rows.indexOf(row)

    this.deleteArming?.abort()
    this.deleteArming = new AbortController()
    button.form?.addEventListener("turbo:submit-start", () => {
      this.lastIndex = index
      this.refocusAfterDelete = true
    }, { once: true, signal: this.deleteArming.signal })

    button.click()
    return true
  }

  // === GRAB, MOVE, DROP ===
  //
  // A grabbed node is rearranged in the DOM and nowhere else until it is
  // dropped. That is what makes Esc a real cancel, and what makes walking a tile
  // five positions one save instead of five.

  // A tile row carries its whole .start-page-item, a group header its whole
  // .start-page-group. An add trigger carries nothing — it sits inside a group,
  // so closest() would otherwise hand it that group to move.
  movableFor(row) {
    if (row.matches(".add-trigger")) return null

    return row.closest(".start-page-item, .start-page-group")
  }

  grab(row) {
    const movable = this.movableFor(row)
    if (!movable) return false

    clearNotice()
    this.grabbed = movable
    this.grabOrigin = { parent: movable.parentElement, nextSibling: movable.nextElementSibling }
    movable.classList.add("grabbed")
    return true
  }

  handleGrabbedKey(event) {
    switch (event.key) {
      case "ArrowDown": return this.shift(1)
      case "ArrowUp": return this.shift(-1)
      case "ArrowRight": return this.shiftColumn(1)
      case "ArrowLeft": return this.shiftColumn(-1)
      case "Enter":
      case " ": return this.drop()
      case "Escape": return this.cancel()
      // Letting go commits, the same rule dragging has. Handled but not
      // swallowed, so the Tab still moves on out of the grid — trapping focus
      // inside a carried row would be a keyboard trap.
      case "Tab": this.drop({ refocus: false }); return false
      default: return false
    }
  }

  isGroup(node) {
    return node.matches(".start-page-group")
  }

  // The list a node sits in. Groups sit in a column, tiles in a group's items.
  zoneOf(node) {
    return this.isGroup(node) ? this.columnOf(node) : node.closest(".group-items")
  }

  // Derived from the zone rather than from what is grabbed, so this stays
  // truthful after the grab has been released.
  siblingsIn(zone) {
    const selector = zone.matches(".start-page-column") ? ".start-page-group" : ".start-page-item"
    return [ ...zone.querySelectorAll(`:scope > ${selector}`) ]
  }

  // A column ends with its "Add group" button, so appending a group means
  // inserting before that, not at the end of the container.
  endMarkerIn(zone) {
    return zone.querySelector(":scope > .inline-form-slot")
  }

  shift(delta) {
    const zone = this.zoneOf(this.grabbed)
    const siblings = this.siblingsIn(zone)
    const neighbour = siblings[siblings.indexOf(this.grabbed) + delta]

    if (neighbour) {
      // Trading places with the neighbour means landing on its far side.
      zone.insertBefore(this.grabbed, delta > 0 ? neighbour.nextSibling : neighbour)
    } else if (this.isGroup(this.grabbed)) {
      return true // A group has nowhere to go past the ends of its column.
    } else if (!this.spillIntoAdjacentGroup(delta)) {
      return true
    }

    this.refocusGrabbed()
    return true
  }

  // Running off the end of a group carries the tile into the next one, which is
  // what makes a column feel like one list to walk down. At the first or last
  // group of the column there is nowhere left to go.
  spillIntoAdjacentGroup(delta) {
    const groups = this.groupsIn(this.columnOf(this.grabbed))
    const current = this.grabbed.closest(".start-page-group")
    const target = groups[groups.indexOf(current) + delta]
    if (!target) return false

    const zone = target.querySelector(".group-items")
    // Coming down from above, land at the top; coming up from below, the bottom.
    zone.insertBefore(this.grabbed, delta > 0 ? this.siblingsIn(zone)[0] ?? null : null)
    return true
  }

  groupsIn(column) {
    return [ ...column.querySelectorAll(":scope > .start-page-group") ]
  }

  shiftColumn(delta) {
    const target = this.columnBeside(this.columnOf(this.grabbed), delta)
    if (!target) return true

    const landed = this.isGroup(this.grabbed)
      ? this.carryGroupInto(target)
      : this.carryItemInto(target)

    if (landed) this.refocusGrabbed()
    return true
  }

  carryGroupInto(column) {
    const index = this.siblingsIn(this.columnOf(this.grabbed)).indexOf(this.grabbed)
    const siblings = this.siblingsIn(column)

    column.insertBefore(this.grabbed, siblings[Math.min(index, siblings.length)] ?? this.endMarkerIn(column))
    return true
  }

  // A tile crossing columns lands in the group beside where it was, not the
  // first one — the same "keep your place on screen" rule the highlight follows.
  carryItemInto(column) {
    const groups = this.groupsIn(column)
    if (groups.length === 0) return false

    const target = this.nearestTo(groups, this.midpointOf(this.grabbed))
    target.querySelector(".group-items").appendChild(this.grabbed)
    return true
  }

  // The focused row travels inside the node that moved, but a browser drops
  // focus when an element is reparented, so put it back.
  refocusGrabbed() {
    const row = this.rowIn(this.grabbed)
    if (row) this.focusRow(row)
  }

  drop({ refocus = true } = {}) {
    const node = this.grabbed
    const zone = this.zoneOf(node)
    const position = this.siblingsIn(zone).indexOf(node)

    this.releaseGrab()

    // The stream answering this replaces the column or the group, so the row
    // holding focus is about to be destroyed. Name the one to focus when it
    // comes back rather than racing the render.
    if (refocus) this.pendingFocusKey = this.keyFor(this.rowIn(node))

    let answered
    if (this.isGroup(node)) {
      node.dataset.groupColumn = zone.dataset.column
      answered = moveGroup(node.dataset.groupId, { column: parseInt(zone.dataset.column), position })
    } else {
      const fromGroupId = node.dataset.groupId
      node.dataset.groupId = zone.dataset.groupId
      answered = moveItem(node.dataset.itemId, { position, fromGroupId, toGroupId: zone.dataset.groupId })
    }

    // Any answer redraws the row somewhere — a refusal puts it back where it
    // really is — so the key gets spent. Only a request that never landed
    // redraws nothing, and an unspent key would lie in wait to claim the next
    // row that happened to match it.
    answered.then(gotAnswer => { if (!gotAnswer) this.pendingFocusKey = null })

    return true
  }

  // Nothing was ever sent, so putting the node back is the whole of the undo.
  cancel() {
    const node = this.grabbed
    const { parent, nextSibling } = this.grabOrigin

    this.releaseGrab()
    parent.insertBefore(node, nextSibling)

    const row = this.rowIn(node)
    if (row) this.focusRow(row)
    return true
  }

  releaseGrab() {
    this.grabbed.classList.remove("grabbed")
    this.grabbed = null
    this.grabOrigin = null
  }
}
