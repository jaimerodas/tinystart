//go:build browser

// The browser suite: the four Capybara system tests, ported to chromedp.
//
// Everything else in this package drives the app with an http.Client, which
// proves what the server answers, not what the page does with it. That gap
// shipped a real bug. lib/start_page_moves.js sends the
// editor's moves as a JSON body, and the handlers only read form values. So
// these tests drive the page the way the page drives itself: real Chrome,
// real keystrokes, real fetches, and the database read afterwards.
//
// They are behind a build tag because they need Chrome on the machine. `go
// test ./...` never sees them. script/test runs them when it finds Chrome.
//
// This file is the harness. The helpers are named after the Capybara ones they
// replace — visit, fillIn, clickOn, sendKeys, assertSelector — so a reader with
// the Ruby open can follow along.
package web

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
	"github.com/jaimerodas/tinystart/internal/store"
)

// One Chrome for the whole test binary, and a tab per test. Starting a browser
// costs the better part of a second. Starting a tab costs milliseconds.
//
// TestMain tears down the allocator through the closeBrowser hook, which is
// the only cleanup that runs after the last test. t.Cleanup ties the browser
// to whichever test happens to open it first, not to the last one.
var (
	browserOnce sync.Once
	browserCtx  context.Context
	browserErr  error
)

func sharedBrowser(t *testing.T) context.Context {
	t.Helper()
	browserOnce.Do(func() {
		options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
		options = append(options,
			// The viewport the Rails system tests ran at. The editor is a
			// desktop grid, and the arrow keys cross columns geometrically. A
			// narrow window changes what "the row beside this one" is.
			chromedp.WindowSize(1400, 1400),
			// Nothing outside this machine has any business being fetched:
			// the layout links Google Fonts, and a tile can point anywhere.
			// Failing to resolve is instant. Waiting for a timeout is not.
			chromedp.Flag("host-resolver-rules", "MAP * ~NOTFOUND, EXCLUDE 127.0.0.1"),
			// Chrome's sandbox needs unprivileged user namespaces, which
			// Ubuntu 24.04 — the GitHub Actions runner — turns off, and Chrome
			// then refuses to start at all ("No usable sandbox!"). This is a
			// throwaway headless browser pointed at localhost. The sandbox
			// protects nothing here.
			chromedp.NoSandbox,
			// chromedp waits 20 seconds for Chrome to announce its DevTools
			// URL, then gives up. A busy CI machine can hold Chrome's start
			// past that, and every test then fails with "websocket url
			// timeout reached". A longer wait costs nothing when Chrome is
			// healthy.
			chromedp.WSURLReadTimeout(time.Minute),
		)
		if path := chromePath(); path != "" {
			options = append(options, chromedp.ExecPath(path))
		}

		allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), options...)
		// WithErrorf drops the error log. chromedp reports every CDP event it
		// has no handler for as an error, and opening the ? dialog emits a
		// stream of dom.EventTopLayerElementsUpdated. Nothing an action
		// returns comes through here — Run answers with its own error — so
		// the only thing lost is that noise.
		ctx, cancelBrowser := chromedp.NewContext(allocator, chromedp.WithErrorf(func(string, ...any) {}))
		// Run with no actions starts the browser, so a machine without Chrome
		// fails here with a plain message rather than inside the first test.
		if err := chromedp.Run(ctx); err != nil {
			browserErr = err
			cancelBrowser()
			cancelAllocator()
			return
		}
		browserCtx = ctx
		closeBrowser = func() {
			cancelBrowser()
			cancelAllocator()
		}
	})
	if browserErr != nil {
		t.Fatalf("starting Chrome: %v", browserErr)
	}
	return browserCtx
}

// chromePath is the Mac install, which chromedp's own search does not know
// about by name. An empty string leaves it to look on PATH.
func chromePath() string {
	const mac = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	if _, err := os.Stat(mac); err == nil {
		return mac
	}
	return ""
}

// browserTimeout bounds every test. It is generous, because a first paint
// includes compiling the app's JavaScript. It is bounded, because a wait
// that hangs must fail the run rather than stall it.
const browserTimeout = 30 * time.Second

// waitTimeout is what an assertion waits for a page to catch up — Capybara's
// default_max_wait_time, which was 2 seconds here. Everything these tests wait
// for is one fetch away on localhost.
const waitTimeout = 5 * time.Second

// browserPage is one tab pointed at one app, plus the assertions the ported
// tests make. ts is exposed on purpose: the point of half these tests is that
// what the page did reached the database.
type browserPage struct {
	t   *testing.T
	ctx context.Context
	ts  *testServer

	mu             sync.Mutex
	confirmAccept  bool
	confirmSeen    []string
	pageExceptions []string
}

// newBrowserPage is a fresh app, a fresh database and a fresh tab, with nobody
// signed in.
func newBrowserPage(t *testing.T) *browserPage {
	t.Helper()
	browser := sharedBrowser(t)
	ts := newTestServer(t)

	ctx, cancelTab := chromedp.NewContext(browser)
	t.Cleanup(cancelTab)
	ctx, cancelTimeout := context.WithTimeout(ctx, browserTimeout)
	t.Cleanup(cancelTimeout)

	p := &browserPage{t: t, ctx: ctx, ts: ts, confirmAccept: true}
	p.listen()

	// Opening the tab here rather than lazily means the first navigation of a
	// test is not also paying for the target to be created. Bringing it to the
	// front is not cosmetic in a headless browser. A background tab is not the
	// focused document, and autofocus — which is how the command bar gets the
	// caret — is skipped for one.
	if err := chromedp.Run(ctx, page.BringToFront()); err != nil {
		t.Fatalf("opening a tab: %v", err)
	}
	return p
}

// startPageBrowser is startPageServer's twin: users(:one), approved, on a grid
// three columns wide, signed in through the real form.
func startPageBrowser(t *testing.T) (*browserPage, *store.User) {
	t.Helper()
	p := newBrowserPage(t)
	user := p.ts.createApprovedUser("one@example.com")
	p.ts.setColumns(user, 3)
	p.signIn(user.Email)
	return p, user
}

// listen watches for two things a browser says. Otherwise a test has to
// guess at them from a timeout: a confirm dialog waiting for an answer, and
// an exception nobody caught.
func (p *browserPage) listen() {
	chromedp.ListenTarget(p.ctx, func(event any) {
		switch e := event.(type) {
		case *page.EventJavascriptDialogOpening:
			p.mu.Lock()
			p.confirmSeen = append(p.confirmSeen, e.Message)
			accept := p.confirmAccept
			p.mu.Unlock()
			// In a goroutine: answering is itself a CDP call, and making one
			// from inside the event handler deadlocks the connection.
			go func() {
				//nolint:errcheck // the tab can already be closed. Nothing to do here
				chromedp.Run(p.ctx, page.HandleJavaScriptDialog(accept))
			}()
		case *runtime.EventExceptionThrown:
			p.mu.Lock()
			p.pageExceptions = append(p.pageExceptions, e.ExceptionDetails.Error())
			p.mu.Unlock()
		}
	})

	// Reported at the end rather than as they happen: a test that already
	// failed is easier to read with the cause underneath it. A test that
	// passed with an exception on the page did not really pass.
	p.t.Cleanup(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, thrown := range p.pageExceptions {
			p.t.Errorf("the page threw: %s", thrown)
		}
	})
}

// === DRIVING ===

func (p *browserPage) run(actions ...chromedp.Action) {
	p.t.Helper()
	if err := chromedp.Run(p.ctx, actions...); err != nil {
		p.t.Fatalf("%v", err)
	}
}

// visit is Capybara's visit: a real navigation, waiting until the page can
// hear what a test does next.
//
// readyState alone is not that. stimulus-loading registers each controller
// through a dynamic import(), which does not hold back "complete". So the
// document can be ready while the controllers are still on their way, and a
// keystroke sent in that gap lands on a page with no listener. CI's slower
// machines hit the gap. So this also waits for every controller the page
// names to be connected.
func (p *browserPage) visit(path string) {
	p.t.Helper()
	p.run(chromedp.Navigate(p.ts.http.URL + path))
	p.waitFor(`document.readyState === "complete" && window.Stimulus &&
		[...document.querySelectorAll("[data-controller]")].every(node =>
			node.getAttribute("data-controller").split(/\s+/).filter(Boolean).every(name =>
				window.Stimulus.getControllerForElementAndIdentifier(node, name)))`,
		"the page to load "+path)
}

func (p *browserPage) currentPath() string {
	p.t.Helper()
	return p.evalString(`location.pathname`)
}

// signIn goes through the form, which is the only way a browser gets a session
// cookie — and is one of the things worth proving works.
func (p *browserPage) signIn(email string) {
	p.t.Helper()
	p.visit("/session/new")
	p.fillIn("#email", email)
	p.fillIn("#password", testPassword)
	p.click(`input[value="Sign in"]`)
	// Wait on the page it lands on rather than on the absence of the one it
	// left. The start page is what everything after this assumes.
	p.assertSelector("main.start-page")
}

// click is a real mouse click at the element's own coordinates, not
// element.click(). A pointer click is what decides :focus-visible, and one of
// the ported tests turns on exactly that difference.
func (p *browserPage) click(selector string) {
	p.t.Helper()
	p.assertSelector(selector)
	p.run(chromedp.Click(selector, chromedp.ByQuery, chromedp.NodeVisible))
}

// clickOn is Capybara's click_button, scoped: the button, submit input or link
// inside scope whose visible text, value or accessible name is the label.
func (p *browserPage) clickOn(scope, label string) {
	p.t.Helper()
	selector := p.evalString(fmt.Sprintf(`(() => {
		const root = %s
		const candidates = [...root.querySelectorAll("button, input[type=submit], a")]
		const nameOf = node => node.textContent.replace(/\s+/g, " ").trim()
		const wanted = candidates.find(node =>
			node.value === %[2]q || node.getAttribute("aria-label") === %[2]q ||
			nameOf(node) === %[2]q)
		if (!wanted) return ""
		// Tagged so the click below addresses this node and no other: several
		// rows on this page carry a button with the same name.
		wanted.dataset.testClick = "1"
		return "[data-test-click]"
	})()`, scopeExpression(scope), label))
	if selector == "" {
		p.t.Fatalf("no button named %q in %s", label, scopeDescription(scope))
	}
	p.run(chromedp.Click(selector, chromedp.ByQuery, chromedp.NodeVisible))
	p.eval(`document.querySelectorAll("[data-test-click]").forEach(n => delete n.dataset.testClick)`, nil)
}

// fillIn is Capybara's fill_in. It selects the field before typing into it,
// so the typing replaces what was there. The typing is real key events,
// which is what the command bar's input listener and the inline forms' Esc
// handler need.
func (p *browserPage) fillIn(selector, value string) {
	p.t.Helper()
	p.assertSelector(selector)
	ok := p.evalBool(fmt.Sprintf(`(() => {
		const field = document.querySelector(%q)
		if (!field || !field.checkVisibility({ checkVisibilityCSS: true })) return false
		field.focus()
		field.select()
		return true
	})()`, selector))
	if !ok {
		p.t.Fatalf("cannot type into %s: it is not visible", selector)
	}
	if value == "" {
		// Nothing to type over the selection with, so delete it.
		p.run(chromedp.KeyEvent(kb.Delete))
		return
	}
	p.run(chromedp.KeyEvent(value))
}

// selectOption is Capybara's select … from: — the value, and the change event
// that a person produces by picking one. Not a click: clicking a <select>
// opens a menu the page cannot see into. The toolbar's picker submits on
// change, which is the whole of the interaction.
func (p *browserPage) selectOption(selector, value string) {
	p.t.Helper()
	p.assertSelector(selector)
	if !p.evalBool(fmt.Sprintf(`(() => {
		const field = document.querySelector(%q)
		if (!field || ![...field.options].some(option => option.value === %q)) return false
		field.focus()
		field.value = %[2]q
		field.dispatchEvent(new Event("change", { bubbles: true }))
		return true
	})()`, selector, value)) {
		p.t.Fatalf("%s has no option %q", selector, value)
	}
}

// fillInLabelled is fill_in by accessible name, which is how the editor's
// forms are addressed. Every field in them carries an aria-label rather than
// a visible <label>.
func (p *browserPage) fillInLabelled(scope, label, value string) {
	p.t.Helper()
	p.fillIn(fmt.Sprintf(`%s [aria-label="%s"]`, scope, label), value)
}

// sendKeys types at whatever holds focus, which is what the grid's keyboard
// model is entirely about. kb has a constant for the named keys. A plain
// string is typed as characters.
func (p *browserPage) sendKeys(keys ...string) {
	p.t.Helper()
	for _, key := range keys {
		p.run(chromedp.KeyEvent(key))
	}
}

// chord is ⌥ held over a physical key: event.code, not event.key. On a Mac,
// ⌥E is a dead key and ⌥S is ß, so the character a chord produces says
// nothing about the pressed key. start_shortcuts_controller matches on the
// code for that reason.
//
// The character the chord types is sent with it, so that "swallowed on the
// page it cannot act on" is a real question. Chrome inserts the text of a
// keyDown unless something calls preventDefault, exactly as it does for
// somebody's actual fingers.
func (p *browserPage) chord(code string, keyCode int64, text string) {
	p.t.Helper()
	const alt = 1
	key := text
	if key == "" {
		key = strings.TrimPrefix(strings.ToLower(code), "key")
	}
	p.run(input.DispatchKeyEvent(input.KeyDown).
		WithModifiers(alt).
		WithCode(code).
		WithKey(key).
		WithText(text).
		WithWindowsVirtualKeyCode(keyCode).
		WithNativeVirtualKeyCode(keyCode))
	p.run(input.DispatchKeyEvent(input.KeyUp).
		WithModifiers(alt).
		WithCode(code).
		WithKey(key).
		WithWindowsVirtualKeyCode(keyCode).
		WithNativeVirtualKeyCode(keyCode))
}

// The two chords the page answers to, with what a Mac keyboard puts under
// them. ⌥E is a dead accent that types nothing on its own, ⌥S is ß.
func (p *browserPage) altE() { p.chord("KeyE", 69, "") }
func (p *browserPage) altS() { p.chord("KeyS", 83, "ß") }

// onConfirm decides what the next data-turbo-confirm gets answered. It is the
// real dialog, handled over CDP, which is what accept_confirm and
// dismiss_confirm were.
func (p *browserPage) onConfirm(accept bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.confirmAccept = accept
}

func (p *browserPage) confirmed() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.confirmSeen...)
}

// waitForConfirm waits until the browser asks, and answers with the
// message it asked. Declining is the case that needs it: nothing happens
// afterwards, so there is no render to wait on instead.
func (p *browserPage) waitForConfirm(count int) []string {
	p.t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for {
		if seen := p.confirmed(); len(seen) >= count {
			return seen
		}
		if time.Now().After(deadline) {
			p.t.Fatalf("waited %s for %d confirm dialogs, saw %d", waitTimeout, count, len(p.confirmed()))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// dismissFlash clicks the flash away. Not decoration: the overlay is
// position: fixed with inset: 0, so while it is up it is what every click on
// the page lands on.
func (p *browserPage) dismissFlash() {
	p.t.Helper()
	p.click(".flash-overlay")
	p.assertNoSelector(".flash-overlay")
}

// attachFile is Capybara's attach_file: the path goes to the file input over
// CDP, because a real file picker cannot be driven from the page.
func (p *browserPage) attachFile(selector, path string) {
	p.t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		p.t.Fatalf("resolving %s: %v", path, err)
	}
	p.run(chromedp.SetUploadFiles(selector, []string{absolute}, chromedp.ByQuery))
}

// === READING ===

func (p *browserPage) eval(expression string, result any) {
	p.t.Helper()
	if err := chromedp.Run(p.ctx, chromedp.Evaluate(expression, result)); err != nil {
		p.t.Fatalf("evaluating %s: %v", expression, err)
	}
}

func (p *browserPage) evalString(expression string) string {
	p.t.Helper()
	var value string
	p.eval(expression+" ?? \"\"", &value)
	return value
}

func (p *browserPage) evalInt(expression string) int {
	p.t.Helper()
	var value int
	p.eval(expression+" ?? 0", &value)
	return value
}

func (p *browserPage) evalBool(expression string) bool {
	p.t.Helper()
	var value bool
	p.eval("!!("+expression+")", &value)
	return value
}

func (p *browserPage) evalStrings(expression string) []string {
	p.t.Helper()
	var values []string
	p.eval(expression, &values)
	return values
}

// texts is every match's visible text, which is what an order assertion on the
// page is made of.
func (p *browserPage) texts(selector string) []string {
	p.t.Helper()
	return p.evalStrings(fmt.Sprintf(
		`[...document.querySelectorAll(%q)].map(n => n.innerText.trim())`, selector))
}

func (p *browserPage) count(selector string) int {
	p.t.Helper()
	return p.evalInt(fmt.Sprintf(`document.querySelectorAll(%q).length`, selector))
}

// === WAITING AND ASSERTING ===
//
// Capybara retries an assertion until it holds or the wait runs out, which is
// what makes a test against an asynchronous page readable. These do the same,
// by asking the page rather than by sleeping. Every one of them is a
// predicate, polled, with the failure that names what it waited for.

func (p *browserPage) waitFor(condition, describe string) {
	p.t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for {
		if p.evalBool(condition) {
			return
		}
		if time.Now().After(deadline) {
			p.t.Fatalf("waited %s for %s", waitTimeout, describe)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// assertSelector is Capybara's: a node that is there and visible.
func (p *browserPage) assertSelector(selector string) {
	p.t.Helper()
	p.waitFor(visibleExpression(selector), "a visible "+selector)
}

func (p *browserPage) assertNoSelector(selector string) {
	p.t.Helper()
	p.waitFor("!("+visibleExpression(selector)+")", "no visible "+selector)
}

// assertPresent is assert_selector visible: :all — the node is in the document
// and nothing is claimed about whether it can be seen. The selected <option>
// of a closed <select> is the case that needs it.
func (p *browserPage) assertPresent(selector string) {
	p.t.Helper()
	p.waitFor(fmt.Sprintf(`document.querySelector(%q)`, selector), "a "+selector)
}

// assertText is assert_selector text: — a match saying it. Because a group
// has several .item-title rows, the one the test waits for is rarely the
// first.
func (p *browserPage) assertText(selector, want string) {
	p.t.Helper()
	p.waitFor(textExpression(selector, want), fmt.Sprintf("%s to say %q", selector, want))
}

// assertNoTextNow is Capybara's wait: 0 — asked once, because waiting for
// something to be absent that is about to appear passes for the wrong reason.
func (p *browserPage) assertNoTextNow(selector, unwanted string) {
	p.t.Helper()
	if p.evalBool(textExpression(selector, unwanted)) {
		p.t.Errorf("%s says %q and should not", selector, unwanted)
	}
}

func textExpression(selector, want string) string {
	return fmt.Sprintf(`[...document.querySelectorAll(%q)]
		.some(node => node.innerText.includes(%q))`, selector, want)
}

func (p *browserPage) assertNoSelectorNow(selector string) {
	p.t.Helper()
	if p.evalBool(visibleExpression(selector)) {
		p.t.Errorf("%s is on the page and should not be", selector)
	}
}

func (p *browserPage) assertCountNow(selector string, want int) {
	p.t.Helper()
	if got := p.count(selector); got != want {
		p.t.Errorf("%s appears %d times, want %d", selector, got, want)
	}
}

// waitForDB polls the database rather than the DOM. Every write on the editor
// is a fetch the page does not wait for, so "the page shows it" and "the
// server stored it" are two different moments. The second is the one these
// tests are for.
func (p *browserPage) waitForDB(describe string, holds func() bool) {
	p.t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for {
		if holds() {
			return
		}
		if time.Now().After(deadline) {
			p.t.Fatalf("waited %s for %s", waitTimeout, describe)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// === FOCUS ===
//
// The editor's keyboard model is a roving tab stop, so most of what these
// tests assert is about document.activeElement. ui-design.md: trust
// evaluate_script over a screenshot for anything measurable.

func (p *browserPage) focusedText() string {
	p.t.Helper()
	return strings.TrimSpace(p.evalString(`document.activeElement.innerText`))
}

func (p *browserPage) focusedTag() string {
	p.t.Helper()
	return strings.ToLower(p.evalString(`document.activeElement.tagName`))
}

func (p *browserPage) focusedLabel() string {
	p.t.Helper()
	return p.evalString(`document.activeElement.getAttribute("aria-label")`)
}

func (p *browserPage) focusedColumn() int {
	p.t.Helper()
	return p.evalInt(`parseInt(document.activeElement.closest(".start-page-column")?.dataset.column)`)
}

func (p *browserPage) focusInsideGrid() bool {
	p.t.Helper()
	return p.evalBool(`document.activeElement.closest("#start_page_grid")`)
}

// enterGrid is the way in the legend offers: Tab from the toolbar's column
// picker, which is the stop before the grid. The select is focused rather than
// clicked because clicking one opens its menu.
func (p *browserPage) enterGrid() {
	p.t.Helper()
	p.assertSelector("#column_count select")
	p.eval(`document.querySelector("#column_count select").focus()`, nil)
	p.sendKeys(kb.Tab)
	p.waitFor(`!!document.activeElement.closest("#start_page_grid")`, "focus to enter the grid")
}

func (p *browserPage) assertFocusedRow(want string) {
	p.t.Helper()
	// The highlight arrives after a render for every move and delete, so this
	// is a wait like any other assertion about the page.
	p.waitFor(fmt.Sprintf(`document.activeElement.innerText.trim() === %q`, want),
		fmt.Sprintf("the highlight to be on %q", want))
}

// === DRAG AND DROP ===

// dragTo is the pointer's way to reorder, driven the only way it can be
// driven. HTML5 drag and drop does not start from synthesised mouse events.
// Chrome begins a drag inside the browser process, out of reach of
// Input.dispatchMouseEvent. That is why the Rails suite left dragging to be
// checked by hand. Capybara's own drag_to has the same problem and solves it
// the same way. It dispatches the DragEvents the page listens for, with a
// real DataTransfer, on the real handle and drop zone.
//
// What that leaves unproven is narrow and unchanged from Rails: the browser's
// own decision to begin a drag from a mousedown on a draggable handle.
// Everything after that — the parting list, the insertion point, the POST and
// what it stores — is exactly what the page runs.
//
// atTop aims the cursor at the top edge of the zone rather than the bottom.
// That is what drag_drop_controller reads to decide where in the list the
// node lands.
func (p *browserPage) dragTo(handleSelector, zoneSelector string, atTop bool) {
	p.t.Helper()
	p.assertPresent(handleSelector)
	p.assertPresent(zoneSelector)

	result := p.evalString(fmt.Sprintf(`(() => {
		const handle = document.querySelector(%q)
		const zone = document.querySelector(%q)
		if (!handle) return "no handle"
		if (!zone) return "no drop zone"

		const box = zone.getBoundingClientRect()
		const clientY = %s
		const clientX = box.left + box.width / 2
		const dataTransfer = new DataTransfer()
		const fire = (node, type) => node.dispatchEvent(new DragEvent(type, {
			bubbles: true, cancelable: true, dataTransfer, clientX, clientY
		}))

		fire(handle, "dragstart")
		fire(zone, "dragover")
		fire(zone, "drop")
		fire(handle, "dragend")
		return ""
	})()`, handleSelector, zoneSelector, dropY(atTop)))
	if result != "" {
		p.t.Fatalf("dragging %s onto %s: %s", handleSelector, zoneSelector, result)
	}
}

func dropY(atTop bool) string {
	if atTop {
		return "box.top + 2"
	}
	return "box.bottom - 2"
}

// === SMALL PLUMBING ===

// visibleExpression is the visibility question asked the way Capybara asks it:
// any match that is in the document and actually rendered.
//
// checkVisibility rather than offsetParent, because a modal <dialog> is
// position: fixed and has no offsetParent while it is very much on screen.
// And checkVisibilityCSS, because the drag handles withdraw with
// visibility: hidden, which the default check counts as visible but a
// reader does not.
func visibleExpression(selector string) string {
	return fmt.Sprintf(`[...document.querySelectorAll(%q)]
		.some(node => node.checkVisibility({ checkVisibilityCSS: true }))`, selector)
}

// scopeExpression turns a CSS scope into the node to search inside. An empty
// scope is the whole document, which is what click_on outside a within block
// meant.
func scopeExpression(scope string) string {
	if scope == "" {
		return "document"
	}
	return fmt.Sprintf(`(document.querySelector(%q) ?? document)`, scope)
}

func scopeDescription(scope string) string {
	if scope == "" {
		return "the page"
	}
	return scope
}

// The selectors for the nodes the editor names. The ids are the ones the
// Turbo Streams target and the keyboard controller focuses by. A test that
// addresses a row this way addresses exactly what the app addresses.
func itemSel(item *store.Item) string    { return "#" + itemDOMID(item.ID) }
func groupSel(group *store.Group) string { return "#" + groupDOMID(group.ID) }
func newItemSel(groupID int64) string    { return "#" + newItemDOMID(groupID) }
func newGroupSel(column int) string      { return "#" + newGroupDOMID(column) }

// tiles is the fixture the Ruby keyboard and shortcuts tests share: one group
// in column 1 with two tiles in it.
func (p *browserPage) tiles(user *store.User) (*store.Group, *store.Item, *store.Item) {
	p.t.Helper()
	group := p.ts.newGroup(user.ID, "Work", 1)
	gmail := p.ts.newItem(user.ID, group.ID, "Gmail", "https://mail.google.com")
	calendar := p.ts.newItem(user.ID, group.ID, "Calendar", "https://calendar.google.com")
	return group, gmail, calendar
}

// positions is what a group's tiles are stored as, in order — the assertion
// that catches a move the page drew and the server never accepted.
func (p *browserPage) positions(groupID int64) []int {
	p.t.Helper()
	items, err := p.ts.db.ItemsInGroup(p.t.Context(), groupID)
	if err != nil {
		p.t.Fatalf("reading group %d: %v", groupID, err)
	}
	found := make([]int, len(items))
	for i, item := range items {
		found[i] = item.Position
	}
	return found
}

// groupPositions is the same for the groups of a column.
func (p *browserPage) groupPositions(userID int64, column int) []int {
	p.t.Helper()
	groups, err := p.ts.db.GroupsInColumn(p.t.Context(), userID, column)
	if err != nil {
		p.t.Fatalf("reading column %d: %v", column, err)
	}
	found := make([]int, len(groups))
	for i, group := range groups {
		found[i] = group.Position
	}
	return found
}

// groupNamed and itemNamed find the row a form on the page just created.
// That is the only way a test learns the id of something it did not make
// itself.
func (p *browserPage) groupNamed(userID int64, column int, name string) *store.Group {
	p.t.Helper()
	groups, err := p.ts.db.GroupsInColumn(p.t.Context(), userID, column)
	if err != nil {
		p.t.Fatalf("reading column %d: %v", column, err)
	}
	for _, group := range groups {
		if group.Name == name {
			return &group
		}
	}
	p.t.Fatalf("no group named %q in column %d", name, column)
	return nil
}

func (p *browserPage) itemNamed(groupID int64, title string) *store.Item {
	p.t.Helper()
	items, err := p.ts.db.ItemsInGroup(p.t.Context(), groupID)
	if err != nil {
		p.t.Fatalf("reading group %d: %v", groupID, err)
	}
	for _, item := range items {
		if item.Title == title {
			return &item
		}
	}
	p.t.Fatalf("no tile named %q in group %d", title, groupID)
	return nil
}

// assertFieldValue is Capybara's assert_field … with: — what a field holds,
// which after a rejected save is the whole question.
func (p *browserPage) assertFieldValue(scope, label, want string) {
	p.t.Helper()
	selector := fmt.Sprintf(`%s [aria-label="%s"]`, scope, label)
	p.assertSelector(selector)
	if got := p.evalString(fmt.Sprintf(`document.querySelector(%q).value`, selector)); got != want {
		p.t.Errorf("%s holds %q, want %q", selector, got, want)
	}
}

// reloadGroup and reloadItem are model.reload: what the row says now.
func (p *browserPage) reloadGroup(userID, id int64) *store.Group {
	p.t.Helper()
	group, err := p.ts.db.GroupByID(p.t.Context(), userID, id)
	if err != nil {
		p.t.Fatalf("reading group %d: %v", id, err)
	}
	return group
}

func (p *browserPage) reloadItem(userID, id int64) *store.Item {
	p.t.Helper()
	item, err := p.ts.db.ItemByID(p.t.Context(), userID, id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		p.t.Fatalf("reading tile %d: %v", id, err)
	}
	return item
}
