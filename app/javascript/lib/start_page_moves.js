// The one way the editor tells the server something moved. Shared by the two
// paths that can move a tile or a group: dragging it with a pointer, and
// picking it up with Space and walking it there with the arrow keys.
//
// Both kinds of move POST the position the node already occupies in the DOM.
// Positions are always compacted 0..n-1 server-side, so an index is a position.

// Resolves to whether the server answered at all — not to whether it allowed
// the move. A refusal still comes back as a stream that says so and redraws the
// truth, so the caller has nothing left to do; only a request that never landed
// leaves the page for its caller to put right.
//
// Note this resolves once the stream has been handed to Turbo, which applies it
// a tick later. Anything that has to run after the DOM has actually changed
// belongs in a Stimulus target callback, not in a .then on this.
async function postMove(url, params) {
  // Whatever the last move had to say about itself, this one supersedes it —
  // otherwise a refusal stays on screen above a move that has since succeeded,
  // still claiming the last thing you did failed.
  clearNotice()

  try {
    const response = await fetch(url, {
      method: "POST",
      headers: headersWithToken(),
      body: JSON.stringify(params)
    })

    if (response.ok || response.status === 422) {
      Turbo.renderStreamMessage(await response.text())
      return true
    }

    showNotice(`Failed to move (${response.status}). Please try again.`)
    return false
  } catch (error) {
    console.error("Error moving:", error)
    showNotice("Could not reach the server. Please try again.")
    return false
  }
}

// The meta tag is absent whenever forgery protection is off — the test
// environment, most obviously. Reading .content straight off the lookup threw a
// TypeError there, which the catch above then reported as an unreachable
// server: a move that never left the browser, blamed on the network.
function headersWithToken() {
  const headers = {
    "Content-Type": "application/json",
    "Accept": "text/vnd.turbo-stream.html"
  }
  const token = document.querySelector('[name="csrf-token"]')?.content
  if (token) headers["X-CSRF-Token"] = token

  return headers
}

// Only for the failures the server never got to answer. Everything else
// arrives as a stream that replaces this same node.
function showNotice(message) {
  const region = document.getElementById("start_page_notice")
  if (!region) return

  region.innerHTML = ""
  const paragraph = document.createElement("p")
  paragraph.className = "start-page-notice-error"
  paragraph.textContent = message
  region.appendChild(paragraph)
}

export function clearNotice() {
  const region = document.getElementById("start_page_notice")
  if (region) region.innerHTML = ""
}

export function moveGroup(groupId, { column, position }) {
  return postMove(`/start/groups/${groupId}/move`, { column, position })
}

// group_id is omitted when it hasn't changed: that is how the server tells a
// reorder within a group from a move between two of them.
export function moveItem(itemId, { position, fromGroupId, toGroupId }) {
  const params = { position }
  if (parseInt(toGroupId) !== parseInt(fromGroupId)) params.group_id = toGroupId

  return postMove(`/start/items/${itemId}/move`, params)
}
