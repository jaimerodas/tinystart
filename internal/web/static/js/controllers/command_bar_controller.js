import { Controller } from "@hotwired/stimulus"
import { trackTileVisit, trackFederatedVisit } from "lib/track_visit"

// Where a query that is not a URL goes. The server validates the choice, and
// an unknown value falls back to DuckDuckGo.
const engines = {
  duckduckgo: { name: "DuckDuckGo", url: "https://duckduckgo.com/?q=" },
  google: { name: "Google", url: "https://www.google.com/search?q=" },
  kagi: { name: "Kagi", url: "https://kagi.com/search?q=" },
}

export default class extends Controller {
  static targets = ["input", "suggestions"]
  // federation: "active" federates to the connected app, "reconnect" says the token was
  // rejected, anything else (no connection) keeps the bar purely local.
  // source: the host those results come from, which names the section.
  // engine: which web search a non-URL query goes to; the default covers the demo page.
  static values = { links: Array, federation: String, source: String, engine: { type: String, default: "duckduckgo" } }

  connect() {
    this.selectedIndex = -1
    this.startPageLinks = []
    this.serverLinks = []
    this.allResults = []
    this.searchTimeout = null
    this.isSearching = false
    this.currentQuery = ""
  }

  search() {
    const query = this.inputTarget.value.trim()
    this.currentQuery = query

    if (query.length === 0) {
      this.clearSearch()
      return
    }

    // Immediately filter and show start page links
    this.startPageLinks = this.filterLocalLinks(query)
    this.renderSuggestions()

    // There is nothing to ask the connected app, or nothing it can answer: the
    // local tiles are the whole result.
    if (this.federationValue !== "active") return

    // Clear existing timeout and set new one for server search
    if (this.searchTimeout) {
      clearTimeout(this.searchTimeout)
    }

    this.isSearching = true
    this.renderSuggestions()

    this.searchTimeout = setTimeout(() => {
      this.fetchServerResults(query)
    }, 500)
  }

  async fetchServerResults(query) {
    // If the query changed while waiting, do not fetch.
    if (query !== this.currentQuery) return

    try {
      const response = await fetch(
        `/search.json?q=${encodeURIComponent(query)}`,
      )
      if (!response.ok) throw new Error("Search failed")

      const results = await response.json()

      // If the query changed during the fetch, do not update.
      if (query !== this.currentQuery) return

      // Exclude links that are already in start page results
      const startPageUrls = new Set(this.startPageLinks.map(l => l.url))
      this.serverLinks = results.filter(link => !startPageUrls.has(link.url))
    } catch (error) {
      console.error("Search error:", error)
      this.serverLinks = []
    } finally {
      this.isSearching = false
      if (query === this.currentQuery) {
        this.renderSuggestions()
      }
    }
  }

  handleKeydown(event) {
    switch(event.key) {
      case 'ArrowDown':
        event.preventDefault()
        this.navigateDown()
        break
      case 'ArrowUp':
        event.preventDefault()
        this.navigateUp()
        break
      case 'Tab':
        if (this.allResults.length > 0) {
          event.preventDefault()
          if (event.shiftKey) {
            this.navigateUp()
          } else {
            this.navigateDown()
          }
        }
        break
      case 'Enter':
        event.preventDefault()
        this.selectCurrent(event.metaKey || event.ctrlKey)
        break
      case 'Escape':
        // The bar is autofocused, so nothing else on this page is reachable by
        // keyboard while it holds focus — ? included. Clearing is the first
        // press. Stepping out is the second press, or the first press on an
        // empty bar.
        if (this.inputTarget.value === "") this.inputTarget.blur()
        this.clearAndHide()
        break
    }
  }

  // A tile matches on its title or on its address. The scheme and a leading
  // "www." are not part of the address here: "https" is in every URL, so one
  // letter of it would match every tile.
  //
  // The most visited match goes first, because it is the one most likely
  // wanted again. Among equals, a title that starts with the query goes before
  // one that only contains it, and then the alphabet decides.
  filterLocalLinks(query) {
    const lowerQuery = query.toLowerCase()
    const address = link => link.url.toLowerCase().replace(/^[a-z][a-z0-9+.-]*:\/\/(www\.)?/, "")

    return this.linksValue.filter(link =>
      link.title.toLowerCase().includes(lowerQuery) || address(link).includes(lowerQuery)
    ).sort((a, b) => {
      if (a.visits !== b.visits) return b.visits - a.visits

      const aTitle = a.title.toLowerCase()
      const bTitle = b.title.toLowerCase()

      const aExact = aTitle.startsWith(lowerQuery)
      const bExact = bTitle.startsWith(lowerQuery)

      if (aExact && !bExact) return -1
      if (!aExact && bExact) return 1

      return aTitle.localeCompare(bTitle)
    })
  }

  // Only called with a query, so there is always at least one row: the one
  // for what was typed. It says what Enter does with the text — "Go to
  // prusa3d.com" or "Search DuckDuckGo for …" — and it is how to get past a
  // matching tile: "prusa3d.com" past a tile for connect.prusa3d.com, a search
  // for "github" past a GitHub tile. It comes before the connected app's
  // results, which arrive later and so cannot push it down.
  //
  // The highlighted row is where Enter goes. The first row starts highlighted:
  // the best tile, or this row when no tile matches.
  renderSuggestions() {
    this.allResults = [
      ...this.startPageLinks.map(l => ({ ...l, section: 'startPage' })),
      { section: "typed" },
      ...this.serverLinks.map(l => ({ ...l, section: 'allLinks' }))
    ]

    this.selectedIndex = Math.min(Math.max(this.selectedIndex, 0), this.allResults.length - 1)

    let html = ''

    // Start Page section
    if (this.startPageLinks.length > 0) {
      html += '<div class="command-bar-section-header">Start Page</div>'
      html += this.startPageLinks.map((link, index) => {
        const isSelected = index === this.selectedIndex
        const selectedClass = isSelected ? 'selected' : ''
        return `<div class="command-bar-suggestion ${selectedClass}" data-index="${index}">
          <span class="suggestion-title">${this.escapeHtml(link.title)}</span>
          <span class="suggestion-url">${this.escapeHtml(link.url)}</span>
        </div>`
      }).join('')
    }

    const typedIndex = this.startPageLinks.length
    const typedClass = typedIndex === this.selectedIndex ? "selected" : ""
    html += `<div class="command-bar-suggestion ${typedClass}" data-index="${typedIndex}">
      <span class="suggestion-title">${this.escapeHtml(this.typedLabel())}</span>
    </div>`

    // Federated section, named after where its results come from
    if (this.isSearching) {
      html += this.federatedHeader()
      html += '<div class="command-bar-searching">Searching...</div>'
    } else if (this.serverLinks.length > 0) {
      html += this.federatedHeader()
      const startOffset = typedIndex + 1
      html += this.serverLinks.map((link, index) => {
        const globalIndex = startOffset + index
        const isSelected = globalIndex === this.selectedIndex
        const selectedClass = isSelected ? "selected" : ""
        return `<div class="command-bar-suggestion ${selectedClass}" data-index="${globalIndex}">
          <span class="suggestion-title">${this.escapeHtml(link.title)}</span>
          <span class="suggestion-url">${this.escapeHtml(link.url)}</span>
        </div>`
      }).join('')
    } else if (this.federationValue === "reconnect") {
      // No header: there is no section to head, just a word about why.
      const what = this.sourceValue ? `${this.escapeHtml(this.sourceValue)} search` : "Search"
      html += `<div class="command-bar-notice">${what} disconnected — reconnect in Settings.</div>`
    }

    this.suggestionsTarget.innerHTML = html;
    this.suggestionsTarget.style.display = "block";

    // Add click handlers
    this.suggestionsTarget
      .querySelectorAll(".command-bar-suggestion")
      .forEach((el) => {
        const index = parseInt(el.dataset.index, 10);
        el.addEventListener("click", () => this.selectSuggestion(index, false));
      });
  }

  typedLabel() {
    const query = this.currentQuery
    return this.isValidUrl(query) ? `Go to ${query}` : `Search ${this.engine.name} for “${query}”`
  }

  federatedHeader() {
    const label = this.sourceValue ? `From ${this.escapeHtml(this.sourceValue)}` : "All Links"
    return `<div class="command-bar-section-header">${label}</div>`
  }

  navigateDown() {
    if (this.allResults.length === 0) return;

    this.selectedIndex = (this.selectedIndex + 1) % this.allResults.length;
    this.updateSelection();
  }

  navigateUp() {
    if (this.allResults.length === 0) return;

    this.selectedIndex =
      this.selectedIndex <= 0
        ? this.allResults.length - 1
        : this.selectedIndex - 1;
    this.updateSelection();
  }

  updateSelection() {
    const suggestions = this.suggestionsTarget.querySelectorAll(
      ".command-bar-suggestion",
    );
    suggestions.forEach((el) => {
      const index = parseInt(el.dataset.index, 10);
      const isSelected = index === this.selectedIndex;
      el.classList.toggle("selected", isSelected);
      if (isSelected) {
        el.scrollIntoView({ block: "nearest" })
      }
    });
  }

  selectCurrent(openInNewTab) {
    if (this.selectedIndex >= 0 && this.allResults.length > 0) {
      this.selectSuggestion(this.selectedIndex, openInNewTab);
    } else {
      this.navigateToUrlOrSearch(this.inputTarget.value, openInNewTab);
    }
  }

  selectSuggestion(index, openInNewTab) {
    const link = this.allResults[index];
    if (link.section === "typed") {
      this.navigateToUrlOrSearch(this.currentQuery, openInNewTab);
      return;
    }
    // Tiles are ours. Everything under "All Links" belongs to the connected app.
    if (link.section === "startPage") {
      trackTileVisit(link.id);
    } else {
      trackFederatedVisit(link.id);
    }
    this.navigateToUrlOrSearch(link.url, openInNewTab);
  }

  navigateToUrlOrSearch(input, openInNewTab) {
    const validatedUrl = this.isValidUrl(input);

    // If it is a valid URL, navigate to it
    const finalUrl = validatedUrl
      ? validatedUrl.href
      : this.buildSearchUrl(input);

    if (openInNewTab) {
      window.open(finalUrl, "_blank");
    } else {
      window.location.href = finalUrl;
    }
  }

  isValidUrl(text) {
    const hasProtocol = /^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//.test(text);
    const normalized = hasProtocol ? text : `https://${text}`;

    try {
      const url = new URL(normalized);

      // Require a dot in the hostname for non-protocol inputs
      if (!hasProtocol && !url.hostname.includes(".")) {
        return null;
      }

      return url;
    } catch {
      return null;
    }
  }

  get engine() {
    return engines[this.engineValue] || engines.duckduckgo;
  }

  buildSearchUrl(query) {
    return this.engine.url + encodeURIComponent(query.trim());
  }

  clearSearch() {
    if (this.searchTimeout) {
      clearTimeout(this.searchTimeout);
    }
    this.startPageLinks = [];
    this.serverLinks = [];
    this.allResults = [];
    this.isSearching = false;
    this.hideSuggestions();
  }

  clearAndHide() {
    this.inputTarget.value = "";
    this.clearSearch();
    this.selectedIndex = -1;
  }

  hideSuggestions() {
    this.suggestionsTarget.style.display = "none";
    this.suggestionsTarget.innerHTML = "";
    this.selectedIndex = -1;
  }

  escapeHtml(text) {
    const div = document.createElement("div");
    div.textContent = text;
    return div.innerHTML;
  }
}
