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
        targetAttribute: 'data-column',
        idField: 'groupId',
        originalField: 'originalColumn'
      }
    } else {
      return {
        draggedElement: this.draggedItem,
        targets: this.itemDropZoneTargets,
        containerSelector: '.group-items',
        targetAttribute: 'data-group-id',
        idField: 'itemId',
        originalField: 'originalGroupId'
      }
    }
  }

  highlightPotentialTargets(dragType) {
    const config = this.getDragConfig(dragType)
    const currentContainer = config.draggedElement.closest(config.containerSelector)
    config.targets.forEach(target => {
      if (target !== currentContainer) {
        target.classList.add("potential-drop-target")
      }
    })
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
    return config.draggedElement && 
           element.hasAttribute(config.targetAttribute) &&
           element !== config.draggedElement.closest(config.containerSelector)
  }

  performOptimisticMove(targetZone, dragType) {
    const config = this.getDragConfig(dragType)
    const draggedElement = config.draggedElement
    
    draggedElement.remove()
    targetZone.appendChild(draggedElement)
    
    // Update data attributes based on type
    if (dragType === 'group') {
      draggedElement.dataset.groupColumn = targetZone.dataset.column
    } else {
      draggedElement.dataset.groupId = targetZone.dataset.groupId
    }
    
    draggedElement.classList.remove("dragging")
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
    
    element.classList.add("dragging")
    this.highlightPotentialTargets(dragType)
    
    return true
  }

  endDrag(dragType) {
    const config = this.getDragConfig(dragType)
    if (config.draggedElement) {
      config.draggedElement.classList.remove("dragging")
    }
    this.clearAllHighlights(dragType)
  }

  handleDragOver(event, dragType) {
    const config = this.getDragConfig(dragType)
    if (!config.draggedElement) return
    
    const targetZone = event.target.closest(config.containerSelector)
    
    if (targetZone && this.isValidDropZone(targetZone, dragType)) {
      event.preventDefault()
      event.dataTransfer.dropEffect = "move"
      this.updateDropZoneHighlight(targetZone)
    } else {
      this.updateDropZoneHighlight(null)
    }
  }

  handleDrop(event, dragType) {
    event.preventDefault()
    
    const targetZone = event.currentTarget
    if (!this.isValidDropZone(targetZone, dragType)) return
    
    if (dragType === 'group') {
      const newColumn = parseInt(targetZone.dataset.column)
      if (newColumn === parseInt(this.draggedData.originalColumn)) return
      
      this.performOptimisticMove(targetZone, dragType)
      
      const newPosition = targetZone.querySelectorAll('.start-page-group').length - 1
      this.makeAPICall(`/start/groups/${this.draggedData.groupId}/move`, {
        column: newColumn,
        position: newPosition
      })
    } else {
      const newGroupId = parseInt(targetZone.dataset.groupId)
      
      this.performOptimisticMove(targetZone, dragType)
      
      const newPosition = targetZone.querySelectorAll('.start-page-item').length - 1
      const params = { position: newPosition }
      if (newGroupId !== parseInt(this.draggedData.originalGroupId)) {
        params.group_id = newGroupId
      }
      
      this.makeAPICall(`/start/items/${this.draggedData.itemId}/move`, params)
    }
    
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
    
    const elementBelow = document.elementFromPoint(touch.clientX, touch.clientY)
    const targetZone = elementBelow?.closest(config.containerSelector)
    const currentZone = draggedElement.closest(config.containerSelector)
      
    config.targets.forEach(zone => {
      if (zone === targetZone && zone !== currentZone) {
        zone.classList.add("drop-zone-active")
      } else {
        zone.classList.remove("drop-zone-active")
      }
    })
  }

  handleTouchEnd(event, dragType) {
    const config = this.getDragConfig(dragType)
    const draggedElement = config.draggedElement
    if (!draggedElement) return
    
    const touch = event.changedTouches[0]
    const elementBelow = document.elementFromPoint(touch.clientX, touch.clientY)
    const targetZone = elementBelow?.closest(config.containerSelector)
    const currentZone = draggedElement.closest(config.containerSelector)
    
    // Reset visual state
    draggedElement.style.transform = ''
    draggedElement.classList.remove("dragging")
    this.clearAllHighlights(dragType)
    
    // Handle drop if valid target
    if (targetZone && targetZone !== currentZone) {
      this.performOptimisticMove(targetZone, dragType)
      
      if (dragType === 'group') {
        const newColumn = parseInt(targetZone.dataset.column)
        const newPosition = targetZone.querySelectorAll('.start-page-group').length - 1
        this.makeAPICall(`/start/groups/${this.draggedData.groupId}/move`, {
          column: newColumn,
          position: newPosition
        })
      } else {
        const newGroupId = parseInt(targetZone.dataset.groupId)
        const newPosition = targetZone.querySelectorAll('.start-page-item').length - 1
        const params = { position: newPosition }
        if (newGroupId !== parseInt(this.draggedData.originalGroupId)) {
          params.group_id = newGroupId
        }
        this.makeAPICall(`/start/items/${this.draggedData.itemId}/move`, params)
      }
    }
    
    // Reset state
    if (dragType === 'group') {
      this.draggedGroup = null
    } else {
      this.draggedItem = null
    }
    this.draggedData = null
    this.touchStartPos = null
  }
}