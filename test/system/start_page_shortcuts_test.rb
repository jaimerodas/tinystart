require "application_system_test_case"

# The chords that move between the two states of the start page, and the ? that
# lists every key either of them answers to.
#
# Both chords are matched on event.code rather than event.key, because on a Mac
# ⌥E is a dead key and ⌥S is ß. Selenium sends them as Alt held over the
# physical key, which is exactly what the controller reads.
class StartPageShortcutsTest < ApplicationSystemTestCase
  def setup
    @user = users(:one)
    sign_in_as(@user)
  end

  # === THE CHORDS ===

  # The command bar has autofocus, so this is also the case that matters: the
  # chord has to work from inside a text field rather than in spite of it.
  test "alt-E opens the editor from the start page" do
    tiles

    visit root_path
    assert_equal "input", focused_tag

    chord("e")

    assert_selector ".editor-toolbar"
    assert_current_path edit_start_path
  end

  test "alt-S goes back to the start page from the editor" do
    tiles

    visit edit_start_path
    chord("s")

    assert_selector ".command-bar"
    assert_current_path root_path
  end

  # Each chord is a no-op on the page it would take you to. It is still
  # swallowed there — otherwise ⌥S would type ß into the search box.
  test "alt-S on the start page goes nowhere and types nothing" do
    tiles

    visit root_path
    chord("s")

    assert_no_selector ".editor-toolbar"
    assert_equal "", command_bar_value
  end

  test "alt-E in the editor goes nowhere" do
    tiles

    visit edit_start_path
    chord("e")

    assert_no_selector ".command-bar"
  end

  # Letting go commits, whichever way you let go. Leaving by chord with a tile
  # still in hand has to save it, exactly as Tab and clicking away already do —
  # otherwise the move is lost on the way out.
  test "alt-S while carrying a tile saves the move on the way out" do
    group = tiles

    visit edit_start_path
    enter_grid
    send_keys(:down)
    assert_equal "Gmail", focused_row_text

    send_keys(:space)
    send_keys(:down)
    chord("s")

    assert_selector ".command-bar"
    # Dropped before the visit is asked for, so the page you land on is already
    # showing the new order rather than the one the move left behind.
    assert_equal [ "Calendar", "Gmail" ], all(".start-page-grid li").map(&:text)
    assert_equal [ "Calendar", "Gmail" ], group.reload.ordered_items.map(&:title)
  end

  # Reading the shortcuts is not letting go of what you are carrying — half of
  # the list is how to move it, and Esc is in there as the way to change your
  # mind. Opening it takes focus the way a click outside the grid does, which is
  # what commits a move, so this is the case that has to be exempt.
  test "asking for the shortcuts mid-carry does not commit the move" do
    group = tiles

    visit edit_start_path
    enter_grid
    send_keys(:down)

    send_keys(:space)
    send_keys(:down)
    send_keys("?")
    assert_selector ".shortcuts-dialog[open]"

    send_keys(:escape)
    assert_no_selector ".shortcuts-dialog[open]"

    # Still in hand, and nothing was saved on the way in or out.
    assert_selector ".start-page-item.grabbed"
    assert_equal [ "Gmail", "Calendar" ], group.reload.ordered_items.map(&:title)
  end

  # === THE DIALOG ===

  # showModal() sets the open attribute, and Turbo photographs the page with it
  # still set. Restoring that snapshot brings the panel back rendered inline —
  # no backdrop, no top layer — where Esc cannot reach it and ? will not reopen
  # something it already believes is open.
  test "the list does not come back open when you navigate back to the page" do
    tiles

    visit edit_start_path
    send_keys("?")
    assert_selector ".shortcuts-dialog[open]"

    # Leaving with it still up is the whole point: that is the page Turbo caches.
    chord("s")
    assert_selector ".command-bar"

    page.go_back

    assert_selector ".editor-toolbar"
    assert_no_selector ".shortcuts-dialog[open]"
  end

  test "? lists the shortcuts and escape closes the list" do
    tiles

    visit edit_start_path
    send_keys("?")

    assert_selector ".shortcuts-dialog[open]"
    assert_text "back to the start page"
    assert_text "pick up / drop"

    send_keys(:escape)
    assert_no_selector ".shortcuts-dialog[open]"
  end

  test "the start page's list names the chord that opens the editor" do
    tiles

    visit root_path
    send_keys(:escape) # off the command bar, which has autofocus
    send_keys("?")

    assert_selector ".shortcuts-dialog[open]"
    assert_text "edit the start page"
  end

  # ? is a shortcut only when nothing is being typed into. The command bar is
  # autofocused, so without this guard it could never be searched for.
  test "? typed into the command bar is a character, not a shortcut" do
    tiles

    visit root_path
    send_keys("?")

    assert_no_selector ".shortcuts-dialog[open]"
    assert_equal "?", command_bar_value
  end

  # And the guard is only survivable because escape gets you off the bar: it is
  # the only way to reach anything else on this page by keyboard.
  test "escape on an empty command bar steps out of it" do
    tiles

    visit root_path
    assert_equal "input", focused_tag

    send_keys(:escape)
    assert_not_equal "input", focused_tag
  end

  test "the first escape clears the bar and the second leaves it" do
    tiles

    visit root_path
    send_keys("gm")
    assert_equal "gm", command_bar_value

    send_keys(:escape)
    assert_equal "", command_bar_value
    assert_equal "input", focused_tag

    send_keys(:escape)
    assert_not_equal "input", focused_tag
  end

  private

  def tiles
    group = @user.start_page_groups.create!(name: "Work", column: 1, position: 0)
    group.start_page_items.create!(url: "https://mail.google.com", title: "Gmail", position: 0)
    group.start_page_items.create!(url: "https://calendar.google.com", title: "Calendar", position: 1)
    group
  end

  # Alt held over a physical key, which is what sets both altKey and the
  # event.code the controller matches on.
  def chord(key)
    page.driver.browser.action.key_down(:alt).send_keys(key).key_up(:alt).perform
  end

  def send_keys(*keys)
    page.driver.browser.action.tap { |a| keys.each { |key| a.send_keys(key) } }.perform
  end

  def enter_grid
    find("#column_count select").send_keys(:tab)
    assert focus_inside_grid?
  end

  def command_bar_value
    evaluate_script("document.querySelector('[data-command-bar-target=\"input\"]').value")
  end

  def focused_tag
    evaluate_script("document.activeElement.tagName").to_s.downcase
  end

  def focused_row_text
    evaluate_script("document.activeElement.innerText").to_s.strip
  end

  def focus_inside_grid?
    evaluate_script("!!document.activeElement.closest('#start_page_grid')")
  end

  def sign_in_as(user)
    visit new_session_path
    fill_in "email", with: user[:email]
    fill_in "password", with: "password123"
    click_button "Sign in"
    assert_selector "main.start-page", wait: 10
  end
end
