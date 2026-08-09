// Fire-and-forget visit recording.
//
// keepalive lets the request finish even when the same click navigates the
// current tab away, so the visit is never lost. Failures are swallowed —
// tracking must never get in the way of opening a link.
//
// Two destinations, because the two kinds of result live in different places:
// tiles belong to this app, federated search results belong to the connected app.
function post(url) {
  const token = document.querySelector('[name="csrf-token"]')?.content
  fetch(url, {
    method: "POST",
    headers: { "X-CSRF-Token": token },
    keepalive: true
  }).catch(() => {})
}

export function trackTileVisit(itemId) {
  if (!itemId) return
  post(`/start/items/${itemId}/visit`)
}

export function trackFederatedVisit(linkId) {
  if (!linkId) return
  post(`/visits?link_id=${encodeURIComponent(linkId)}`)
}
