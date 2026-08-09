import { Controller } from "@hotwired/stimulus"
import { trackTileVisit, trackFederatedVisit } from "lib/track_visit"

export default class extends Controller {
  static targets = ["input", "suggestions"]
  // federation: "active" federates to the connected app, "reconnect" says the token was
  // rejected, anything else (no connection) keeps the bar purely local.
  // source: the host those results come from, which names the section.
  static values = { links: Array, federation: String, source: String }

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

    // Nothing to ask the connected app, or nothing it would answer: the local tiles are
    // the whole result.
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
    // Don't fetch if query changed while waiting
    if (query !== this.currentQuery) return

    try {
      const response = await fetch(
        `/search.json?q=${encodeURIComponent(query)}`,
      )
      if (!response.ok) throw new Error("Search failed")

      const results = await response.json()

      // Don't update if query changed during fetch
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
        this.clearAndHide()
        break
    }
  }

  filterLocalLinks(query) {
    const lowerQuery = query.toLowerCase()

    return this.linksValue.filter(link =>
      link.title.toLowerCase().includes(lowerQuery)
    ).sort((a, b) => {
      const aTitle = a.title.toLowerCase()
      const bTitle = b.title.toLowerCase()

      const aExact = aTitle.startsWith(lowerQuery)
      const bExact = bTitle.startsWith(lowerQuery)

      if (aExact && !bExact) return -1
      if (!aExact && bExact) return 1

      return aTitle.localeCompare(bTitle)
    })
  }

  renderSuggestions() {
    // Build combined results list for keyboard navigation
    this.allResults = [
      ...this.startPageLinks.map(l => ({ ...l, section: 'startPage' })),
      ...this.serverLinks.map(l => ({ ...l, section: 'allLinks' }))
    ]

    // Hide if nothing to show
    if (this.allResults.length === 0 && !this.isSearching) {
      this.hideSuggestions()
      return
    }

    // Auto-select first Start Page result only (not All Links)
    if (this.selectedIndex === -1 && this.startPageLinks.length > 0) {
      this.selectedIndex = 0
    }

    // Ensure selectedIndex is within bounds (but allow -1 for no selection)
    if (this.selectedIndex >= this.allResults.length) {
      this.selectedIndex = this.allResults.length > 0 ? this.allResults.length - 1 : -1
    }

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

    // Federated section, named after where its results come from
    if (this.isSearching) {
      html += this.federatedHeader()
      html += '<div class="command-bar-searching">Searching...</div>'
    } else if (this.serverLinks.length > 0) {
      html += this.federatedHeader()
      const startOffset = this.startPageLinks.length
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
    // Tiles are ours; everything under "All Links" belongs to the connected app.
    if (link.section === "startPage") {
      trackTileVisit(link.id);
    } else {
      trackFederatedVisit(link.id);
    }
    this.navigateToUrlOrSearch(link.url, openInNewTab);
  }

  navigateToUrlOrSearch(input, openInNewTab) {
    const validatedUrl = this.isValidUrl(input);

    // If it's a valid URL, navigate to it
    const finalUrl = validatedUrl
      ? validatedUrl.href
      : this.buildDuckDuckGoSearchUrl(input);

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

  buildDuckDuckGoSearchUrl(query) {
    const encodedQuery = encodeURIComponent(query.trim());
    return `https://duckduckgo.com/?q=${encodedQuery}`;
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
