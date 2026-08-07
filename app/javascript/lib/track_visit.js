// Fire-and-forget POST that records a visit to a start page tile.
//
// keepalive lets the request finish even when the same click navigates the
// current tab away, so the visit is never lost. Failures are swallowed —
// tracking must never get in the way of opening a link.
export function trackVisit(itemId) {
  if (!itemId) return

  const token = document.querySelector('[name="csrf-token"]')?.content
  fetch(`/start/items/${itemId}/visit`, {
    method: "POST",
    headers: { "X-CSRF-Token": token },
    keepalive: true
  }).catch(() => {})
}
