import { Controller } from "@hotwired/stimulus"

export default class extends Controller {
  static targets = ["group", "column", "handle", "item", "itemHandle", "itemDropZone"]

  connect() {
    this.draggedGroup = null
    this.draggedItem = null
    this.draggedData = null
    this.touchStartPos = null
    this.isTouchDevice = 'ontouchstart' in window
    this.currentDropZone = null
    
    if (this.isTouchDevice) {
      this.addTouchListeners()
    }
  }

  // === COMMON HELPER METHODS ===

  addTouchListeners() {
    this.handleTargets.forEach(handle => {
      handle.addEventListener('touchstart', (e) => this.handleTouchStart(e, 'group'), { passive: false })
      handle.addEventListener('touchmove', (e) => this.handleTouchMove(e, 'group'), { passive: false })
      handle.addEventListener('touchend', (e) => this.handleTouchEnd(e, 'group'), { passive: false })
    })

    this.itemHandleTargets.forEach(handle => {
      handle.addEventListener('touchstart', (e) => this.handleTouchStart(e, 'item'), { passive: false })
      handle.addEventListener('touchmove', (e) => this.handleTouchMove(e, 'item'), { passive: false })
      handle.addEventListener('touchend', (e) => this.handleTouchEnd(e, 'item'), { passive: false })
    })
  }

  getDragConfig(dragType) {
    if (dragType === 'group') {
      return {
        draggedElement: this.draggedGroup,
        targets: this.columnTargets,
        containerSelector: '.start-page-column',
        childSelector: '.start-page-group',
        // A column ends with its "Add group" button, so the end of the list is
        // not the end of the container.
        endMarkerSelector: '.inline-form-slot',
        targetAttribute: 'data-column'
      }
    } else {
      return {
        draggedElement: this.draggedItem,
        targets: this.itemDropZoneTargets,
        containerSelector: '.group-items',
        childSelector: '.start-page-item',
        endMarkerSelector: null,
        targetAttribute: 'data-group-id'
      }
    }
  }

  // Every zone is a target, including the one being dragged from — that is what
  // reordering in place means.
  highlightPotentialTargets(dragType) {
    const config = this.getDragConfig(dragType)
    config.targets.forEach(target => target.classList.add("potential-drop-target"))
  }

  // === INSERTION POINT ===

  childrenIn(zone, dragType) {
    const config = this.getDragConfig(dragType)
    return [...zone.querySelectorAll(`:scope > ${config.childSelector}`)]
  }

  // What to insert before so the element lands under the cursor: the first
  // sibling whose midpoint the cursor has passed, or the end of the list.
  insertionReference(zone, clientY, dragType) {
    const config = this.getDragConfig(dragType)

    const below = this.childrenIn(zone, dragType).find(child => {
      if (child === config.draggedElement) return false
      const rect = child.getBoundingClientRect()
      return clientY < rect.top + rect.height / 2
    })
    if (below) return below

    return config.endMarkerSelector
      ? zone.querySelector(`:scope > ${config.endMarkerSelector}`)
      : null
  }

  // Moving the element while the drag is still going is what makes the list
  // part around it. The guard keeps a held cursor from re-inserting on every
  // dragover tick.
  previewInsertion(zone, clientY, dragType) {
    const dragged = this.getDragConfig(dragType).draggedElement
    const reference = this.insertionReference(zone, clientY, dragType)

    if (dragged.parentElement === zone && dragged.nextElementSibling === reference) return

    zone.insertBefore(dragged, reference)
  }

  // Settles the element where the cursor left it and reports the index it
  // landed on. Positions are always compacted, so the index is the position.
  placeAt(zone, clientY, dragType) {
    const dragged = this.getDragConfig(dragType).draggedElement
    this.previewInsertion(zone, clientY, dragType)

    if (dragType === 'group') {
      dragged.dataset.groupColumn = zone.dataset.column
    } else {
      dragged.dataset.groupId = zone.dataset.groupId
    }
    dragged.classList.remove("dragging")

    return this.childrenIn(zone, dragType).indexOf(dragged)
  }

  requestMove(zone, position, dragType) {
    if (dragType === 'group') {
      this.makeAPICall(`/start/groups/${this.draggedData.groupId}/move`, {
        column: parseInt(zone.dataset.column),
        position: position
      })
    } else {
      const params = { position: position }
      const newGroupId = parseInt(zone.dataset.groupId)
      // Omitted when it hasn't changed: that is how the server tells a reorder
      // from a move between groups.
      if (newGroupId !== parseInt(this.draggedData.originalGroupId)) {
        params.group_id = newGroupId
      }
      this.makeAPICall(`/start/items/${this.draggedData.itemId}/move`, params)
    }
  }

  updateDropZoneHighlight(newZone) {
    if (this.currentDropZone !== newZone) {
      if (this.currentDropZone) {
        this.currentDropZone.classList.remove("drop-zone-active")
      }
      if (newZone) {
        newZone.classList.add("drop-zone-active")
      }
      this.currentDropZone = newZone
    }
  }

  clearAllHighlights(dragType) {
    const config = this.getDragConfig(dragType)
    config.targets.forEach(target => {
      target.classList.remove("drop-zone-active")
      target.classList.remove("potential-drop-target")
    })
    this.currentDropZone = null
  }

  isValidDropZone(element, dragType) {
    const config = this.getDragConfig(dragType)
    return Boolean(config.draggedElement) && element.hasAttribute(config.targetAttribute)
  }

  async makeAPICall(url, params) {
    try {
      const response = await fetch(url, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Accept': 'text/vnd.turbo-stream.html',
          'X-CSRF-Token': document.querySelector('[name="csrf-token"]').content
        },
        body: JSON.stringify(params)
      })

      if (response.ok) {
        const turboStreamContent = await response.text()
        Turbo.renderStreamMessage(turboStreamContent)
      } else {
        console.error('Failed to move:', response.statusText)
        this.showErrorMessage('Failed to move. Please try again.')
      }
    } catch (error) {
      console.error('Error moving:', error)
      this.showErrorMessage('Error moving. Please try again.')
    }
  }

  showErrorMessage(message) {
    const errorElement = document.createElement('div')
    errorElement.className = 'alert alert-error'
    errorElement.style.cssText = 'margin: 1em 0; padding: 1em; background: var(--danger-bg-color, #fef2f2); border: 1px solid var(--danger-color, #ef4444); border-radius: 0.5em; color: var(--danger-color, #ef4444);'
    errorElement.textContent = message
    
    const main = document.querySelector('main')
    main.insertBefore(errorElement, main.firstChild)
    
    setTimeout(() => errorElement.remove(), 5000)
  }

  // === UNIFIED DRAG METHODS ===

  initiateDrag(event, dragType) {
    const config = this.getDragConfig(dragType)
    const element = event.currentTarget.closest(dragType === 'group' ? '.start-page-group' : '.start-page-item')
    
    if (!element) {
      event.preventDefault()
      return false
    }
    
    if (dragType === 'group') {
      this.draggedGroup = element
      this.draggedData = {
        groupId: element.dataset.groupId,
        originalColumn: element.dataset.groupColumn
      }
    } else {
      this.draggedItem = element
      this.draggedData = {
        itemId: element.dataset.itemId,
        originalGroupId: element.dataset.groupId,
        originalPosition: parseInt(element.dataset.itemPosition)
      }
    }
    
    if (event.dataTransfer) {
      event.dataTransfer.effectAllowed = "move"
      event.dataTransfer.setData("text/html", element.outerHTML)
    }

    // The list parts around the element as it is dragged, which means the DOM
    // has already changed by the time a drag is abandoned. Remember where it
    // came from so an abandoned one can be put back.
    this.dragOrigin = { parent: element.parentElement, nextSibling: element.nextElementSibling }
    this.dropped = false

    element.classList.add("dragging")
    this.highlightPotentialTargets(dragType)

    return true
  }

  endDrag(dragType) {
    const config = this.getDragConfig(dragType)
    if (config.draggedElement) {
      config.draggedElement.classList.remove("dragging")
      if (!this.dropped) this.restoreDragOrigin(config.draggedElement)
    }
    this.clearAllHighlights(dragType)
    this.dragOrigin = null
    this.forgetDragged(dragType)
  }

  // Both kinds of drop zone are nested — .group-items sits inside
  // .start-page-column — so every drag event reaches both sets of handlers. A
  // reference left over from a finished drag makes the wrong one act on it, and
  // isValidDropZone no longer filters by source container to catch that.
  forgetDragged(dragType) {
    if (dragType === 'group') {
      this.draggedGroup = null
    } else {
      this.draggedItem = null
    }
    this.draggedData = null
  }

  // Dropped on nothing, or cancelled with Esc: the server was never told, so
  // the page must not keep the rearrangement either.
  restoreDragOrigin(element) {
    if (!this.dragOrigin) return
    this.dragOrigin.parent.insertBefore(element, this.dragOrigin.nextSibling)
  }

  handleDragOver(event, dragType) {
    const config = this.getDragConfig(dragType)
    if (!config.draggedElement) return
    
    const targetZone = event.target.closest(config.containerSelector)
    
    if (targetZone && this.isValidDropZone(targetZone, dragType)) {
      event.preventDefault()
      event.dataTransfer.dropEffect = "move"
      this.updateDropZoneHighlight(targetZone)
      this.previewInsertion(targetZone, event.clientY, dragType)
    } else {
      this.updateDropZoneHighlight(null)
    }
  }

  handleDrop(event, dragType) {
    event.preventDefault()

    const targetZone = event.currentTarget
    if (!this.isValidDropZone(targetZone, dragType)) return

    this.dropped = true
    this.requestMove(targetZone, this.placeAt(targetZone, event.clientY, dragType), dragType)

    targetZone.classList.remove("drop-zone-active")
  }

  // === GROUP DRAG METHODS ===

  dragStart(event) {
    this.initiateDrag(event, 'group')
  }

  dragEnd(event) {
    this.endDrag('group')
  }

  dragOver(event) {
    this.handleDragOver(event, 'group')
  }

  drop(event) {
    this.handleDrop(event, 'group')
  }

  // === ITEM DRAG METHODS ===

  dragItemStart(event) {
    this.initiateDrag(event, 'item')
  }

  dragItemEnd(event) {
    this.endDrag('item')
  }

  dragItemOver(event) {
    this.handleDragOver(event, 'item')
  }

  dropItem(event) {
    this.handleDrop(event, 'item')
  }

  // === TOUCH EVENT HANDLERS ===

  handleTouchStart(event, dragType) {
    this.touchStartPos = {
      x: event.touches[0].clientX,
      y: event.touches[0].clientY
    }
    
    if (this.initiateDrag(event, dragType)) {
    }
  }

  handleTouchMove(event, dragType) {
    event.preventDefault()
    
    const config = this.getDragConfig(dragType)
    const draggedElement = config.draggedElement
    if (!draggedElement || !this.touchStartPos) return
    
    const touch = event.touches[0]
    const deltaX = touch.clientX - this.touchStartPos.x
    const deltaY = touch.clientY - this.touchStartPos.y
    
    draggedElement.style.transform = `translate(${deltaX}px, ${deltaY}px)`

    const targetZone = this.zoneUnderPoint(touch.clientX, touch.clientY, dragType)

    config.targets.forEach(zone => {
      zone.classList.toggle("drop-zone-active", zone === targetZone)
    })
  }

  // A touch drag carries the element under the finger, so asking what is at that
  // point answers "the thing you are dragging" and every drop resolves to the
  // zone it came from. Put it back and take it out of hit testing first.
  zoneUnderPoint(x, y, dragType) {
    const config = this.getDragConfig(dragType)
    const dragged = config.draggedElement
    const restore = dragged.style.pointerEvents

    dragged.style.pointerEvents = 'none'
    const below = document.elementFromPoint(x, y)
    dragged.style.pointerEvents = restore

    return below?.closest(config.containerSelector)
  }

  handleTouchEnd(event, dragType) {
    const config = this.getDragConfig(dragType)
    const draggedElement = config.draggedElement
    if (!draggedElement) return

    const touch = event.changedTouches[0]

    draggedElement.style.transform = ''
    const targetZone = this.zoneUnderPoint(touch.clientX, touch.clientY, dragType)

    draggedElement.classList.remove("dragging")
    this.clearAllHighlights(dragType)

    if (targetZone) {
      this.dropped = true
      this.requestMove(targetZone, this.placeAt(targetZone, touch.clientY, dragType), dragType)
    } else {
      this.restoreDragOrigin(draggedElement)
    }
    this.dragOrigin = null
    this.forgetDragged(dragType)
    this.touchStartPos = null
  }
}