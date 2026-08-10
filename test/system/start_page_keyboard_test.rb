require "application_system_test_case"

# The first reorder path a test can drive. HTML5 drag events do not respond to
# Selenium's synthetic mouse events, which is why the drag half of this feature
# is still verified by hand — but send_keys has no such problem, so everything
# the keyboard can do to the grid is checked here against both the DOM and the
# database.
class StartPageKeyboardTest < ApplicationSystemTestCase
  def setup
    @user = users(:one)
    sign_in_as(@user)
  end

  # === NAVIGATION ===

  test "the whole grid is one tab stop" do
    tiles

    visit edit_start_path
    enter_grid

    assert_equal "Work", focused_row_text

    # Out the other side in one press, however many tiles are on the page.
    send_keys(:tab)
    assert_not focus_inside_grid?
  end

  test "arrow keys walk the rows of a column and cross between columns" do
    tiles

    visit edit_start_path
    enter_grid

    assert_equal "Work", focused_row_text
    send_keys(:down)
    assert_equal "Gmail", focused_row_text
    send_keys(:down)
    assert_equal "Calendar", focused_row_text
    send_keys(:up)
    assert_equal "Gmail", focused_row_text

    send_keys(:right)
    assert_equal 2, focused_column
  end

  test "arrowing down past the last tile of a group reaches its add trigger" do
    tiles

    visit edit_start_path
    enter_grid

    send_keys(:down, :down, :down)
    assert_equal "Add link", focused_row_text
  end

  test "the highlight stops at the ends rather than wrapping" do
    tiles

    visit edit_start_path
    enter_grid

    send_keys(:up, :up, :up)
    assert_equal "Work", focused_row_text

    send_keys(:end)
    last = focused_row_text
    send_keys(:down, :down)
    assert_equal last, focused_row_text
  end

  # === KEYBOARD MODE ===
  #
  # The keys do nothing until focus is in the grid, so the legend has to say how
  # to get in before it says what is available once you are.

  test "the legend offers the way in, then switches to the keys once you are in" do
    tiles

    visit edit_start_path
    assert_selector ".keyboard-legend-enter", text: "to enter keyboard mode"
    assert_no_selector ".keyboard-legend-keys"

    enter_grid

    assert_selector ".keyboard-legend-keys", text: "for all the shortcuts"
    assert_no_selector ".keyboard-legend-enter"
  end

  # Two ways to move the same tile at the same time is one too many — and a
  # handle is unreachable by keyboard anyway, so in keyboard mode it is clutter.
  test "the drag handles withdraw in keyboard mode and come back on the way out" do
    tiles

    visit edit_start_path
    assert_selector "#column_1 .drag-handle"

    enter_grid
    assert_no_selector "#column_1 .drag-handle"

    send_keys(:tab)
    assert_not focus_inside_grid?
    assert_selector "#column_1 .drag-handle"
  end

  # Clicking a row focuses it, but a pointer user has not asked for keyboard
  # mode and must not lose the handles for reaching one.
  test "clicking a row does not take the drag handles away" do
    group = tiles

    visit edit_start_path
    find("#item_#{gmail(group).id} .item-title").click

    assert_selector "#column_1 .drag-handle"
    assert_no_selector ".keyboard-legend-keys"
  end

  # === ACTIONS ===

  test "enter opens a tile's form and escape hands focus back to its row" do
    group = tiles

    visit edit_start_path
    enter_grid
    send_keys(:down)

    send_keys(:enter)
    assert_equal "Gmail", page.find("#item_#{gmail(group).id} input[aria-label='Title']").value
    assert_equal "input", focused_tag

    send_keys(:escape)
    assert_equal "Gmail", focused_row_text
  end

  test "enter on the add trigger opens the form for a new link" do
    tiles

    visit edit_start_path
    enter_grid

    send_keys(:down, :down, :down)
    assert_equal "Add link", focused_row_text

    send_keys(:enter)
    assert_equal "input", focused_tag
    assert_equal "Title", focused_label
  end

  test "delete removes a tile and leaves the highlight where it was" do
    group = tiles
    target = gmail(group)

    visit edit_start_path
    enter_grid
    send_keys(:down)
    assert_equal "Gmail", focused_row_text

    accept_confirm { send_keys(:delete) }

    assert_no_selector "#item_#{target.id}"
    assert_equal [ "Calendar" ], group.reload.ordered_items.map(&:title)
    # The row that took its place, not the top of the document.
    assert_equal "Calendar", focused_row_text
  end

  # === REORDERING ===

  test "space picks a tile up, an arrow moves it, and space saves it once" do
    group = tiles

    visit edit_start_path
    enter_grid
    send_keys(:down)

    send_keys(:space)
    assert_selector "#item_#{gmail(group).id}.grabbed"

    send_keys(:down)
    send_keys(:space)

    assert_no_selector ".grabbed"
    assert_selector "#group_#{group.id} .start-page-item:first-of-type", text: "Calendar"
    assert_equal [ "Calendar", "Gmail" ], group.reload.ordered_items.map(&:title)
    assert_equal [ 0, 1 ], group.ordered_items.map(&:position)
    # The highlight followed the tile through the re-render.
    assert_equal "Gmail", focused_row_text
  end

  # Letting go commits, the same rule Tab follows. The alternative is a move
  # left dangling on screen, to be silently committed by whatever the user
  # clicks next or silently lost if they navigate away.
  test "clicking away while carrying commits the move" do
    group = tiles

    visit edit_start_path
    enter_grid
    send_keys(:down)

    send_keys(:space)
    send_keys(:down)

    # The legend is outside the grid and focuses nothing, so this is a bare
    # departure rather than a move to another row.
    find(".keyboard-legend").click

    assert_no_selector ".grabbed"
    assert_selector "#group_#{group.id} .start-page-item:first-of-type", text: "Calendar"
    assert_equal [ "Calendar", "Gmail" ], group.reload.ordered_items.map(&:title)
  end

  # A move destroys the focused row and focus sits on <body> until the render
  # brings it back. Reading that as leaving flickers the handles back and swaps
  # the legend twice on every single move.
  test "keyboard mode survives the round trip of a move" do
    group = tiles

    visit edit_start_path
    enter_grid
    send_keys(:down)

    send_keys(:space)
    send_keys(:down)
    send_keys(:space)

    # Capybara retries the DOM, not the database, so wait on the render first.
    assert_selector "#group_#{group.id} .start-page-item:first-of-type", text: "Calendar"
    assert_equal [ "Calendar", "Gmail" ], group.reload.ordered_items.map(&:title)

    assert_selector ".start-page-grid.keyboard-mode"
    assert_no_selector "#column_1 .drag-handle"
    assert_selector ".keyboard-legend-keys"
  end

  test "escape during a move puts the tile back and saves nothing" do
    group = tiles

    visit edit_start_path
    enter_grid
    send_keys(:down)

    send_keys(:space)
    send_keys(:down)
    # Already rearranged on screen, but nothing has been sent yet.
    assert_selector "#group_#{group.id} .start-page-item:first-of-type", text: "Calendar"

    send_keys(:escape)

    assert_no_selector ".grabbed"
    assert_selector "#group_#{group.id} .start-page-item:first-of-type", text: "Gmail"
    assert_equal [ "Gmail", "Calendar" ], group.reload.ordered_items.map(&:title)
    assert_equal "Gmail", focused_row_text
  end

  test "a tile carried past the end of its group spills into the next one" do
    group = tiles
    other = @user.start_page_groups.create!(name: "Reading", column: 1, position: 1)
    other.start_page_items.create!(url: "https://example.com/rss", title: "RSS", position: 0)

    visit edit_start_path
    enter_grid
    send_keys(:down, :down)
    assert_equal "Calendar", focused_row_text

    calendar = group.start_page_items.find_by(title: "Calendar")
    send_keys(:space)
    send_keys(:down)
    send_keys(:space)

    assert_selector "#group_#{other.id} #item_#{calendar.id}"
    assert_equal other, calendar.reload.start_page_group
    assert_equal [ "Calendar", "RSS" ], other.reload.ordered_items.map(&:title)
    assert_equal [ "Gmail" ], group.reload.ordered_items.map(&:title)
    assert_equal [ 0 ], group.ordered_items.map(&:position)
  end

  test "a group can be carried into the next column" do
    group = tiles

    visit edit_start_path
    enter_grid
    assert_equal "Work", focused_row_text

    send_keys(:space)
    send_keys(:right)
    send_keys(:space)

    assert_selector "#column_2 #group_#{group.id}"
    assert_equal 2, group.reload.column
    assert_equal 0, group.position
  end

  test "a group reorders within its column" do
    first = tiles
    second = @user.start_page_groups.create!(name: "Reading", column: 1, position: 1)

    visit edit_start_path
    enter_grid

    send_keys(:space)
    send_keys(:down)
    send_keys(:space)

    # Capybara retries the DOM, not the database, so wait on the render first.
    assert_selector "#column_1 .start-page-group:first-of-type .group-name", text: "Reading"
    assert_equal [ "Reading", "Work" ], @user.groups_in_column(1).map(&:name)
    assert_equal [ 0, 1 ], [ second, first ].map { |g| g.reload.position }
  end

  test "carrying a tile past the top of the first group saves nothing" do
    group = tiles

    visit edit_start_path
    enter_grid
    send_keys(:down)

    send_keys(:space)
    send_keys(:up)
    send_keys(:up)
    send_keys(:space)

    # It never left position 0, so the order is what it always was.
    assert_equal [ "Gmail", "Calendar" ], group.reload.ordered_items.map(&:title)
  end

  # A move that cannot land has to say so. It used to answer 200 with a stream
  # aimed at an id rendered nowhere, so nothing appeared and the client's
  # response.ok check passed.
  test "a rejected move reports itself in the notice region" do
    group = tiles
    other = @user.start_page_groups.create!(name: "Reading", column: 1, position: 1)
    other.start_page_items.create!(url: "https://mail.google.com", title: "Mail", position: 0)

    visit edit_start_path
    enter_grid
    send_keys(:down)

    send_keys(:space)
    send_keys(:down)
    send_keys(:down)
    send_keys(:space)

    within("#start_page_notice") { assert_text "Failed to move item." }
    assert_equal group, gmail(group).reload.start_page_group

    # The client moved it before it asked, so the refusal has to put it back —
    # otherwise the page shows an order the database refused, and the next move
    # computes its position from that page.
    assert_selector "#group_#{group.id} #item_#{gmail(group).id}"
    assert_no_selector "#group_#{other.id} #item_#{gmail(group).id}"
  end

  # A notice that outlives its failure is worse than none: it reports the last
  # thing you did as broken when it worked.
  test "a later successful move clears the notice the failed one left" do
    group = tiles
    other = @user.start_page_groups.create!(name: "Reading", column: 1, position: 1)
    other.start_page_items.create!(url: "https://mail.google.com", title: "Mail", position: 0)

    visit edit_start_path
    enter_grid
    send_keys(:down)

    send_keys(:space)
    send_keys(:down, :down)
    send_keys(:space)
    within("#start_page_notice") { assert_text "Failed to move item." }

    # Somewhere it is allowed to go.
    send_keys(:space)
    send_keys(:down)
    send_keys(:space)

    assert_no_selector ".start-page-notice-error"
    assert_equal [ "Calendar", "Gmail" ], group.reload.ordered_items.map(&:title)
  end

  # Declining the confirm submits nothing, so nothing will ever redeem a
  # highlight promised for "after the delete" — and the next unrelated render
  # would redeem it by yanking focus out of whatever was open by then.
  test "declining a delete does not leave the highlight owing" do
    group = tiles

    visit edit_start_path
    enter_grid
    send_keys(:down)

    dismiss_confirm { send_keys(:delete) }
    assert_equal [ "Gmail", "Calendar" ], group.reload.ordered_items.map(&:title)

    # Open the add form, which re-renders the group — the focus promise, if it
    # were still owing, gets redeemed here and steals the field.
    send_keys(:down, :down)
    assert_equal "Add link", focused_row_text
    send_keys(:enter)
    fill_in "Title", with: "Docs"
    fill_in "URL", with: "https://docs.example.com"
    click_button "Add"

    assert_selector "#group_#{group.id} .item-title", text: "Docs"
    assert_equal "input", focused_tag
  end

  private

  def tiles
    group = @user.start_page_groups.create!(name: "Work", column: 1, position: 0)
    group.start_page_items.create!(url: "https://mail.google.com", title: "Gmail", position: 0)
    group.start_page_items.create!(url: "https://calendar.google.com", title: "Calendar", position: 1)
    group
  end

  def gmail(group)
    group.start_page_items.find_by(title: "Gmail")
  end

  # Tab from the toolbar's column picker, which is the stop before the grid.
  def enter_grid
    find("#column_count select").send_keys(:tab)
    assert focus_inside_grid?
  end

  def send_keys(*keys)
    page.driver.browser.action.tap { |a| keys.each { |key| a.send_keys(key) } }.perform
  end

  # The rows carry their label as text, so this is what a user sees highlighted.
  def focused_row_text
    evaluate_script("document.activeElement.innerText").to_s.strip
  end

  def focused_tag
    evaluate_script("document.activeElement.tagName").to_s.downcase
  end

  def focused_label
    evaluate_script("document.activeElement.getAttribute('aria-label')")
  end

  def focused_column
    evaluate_script("document.activeElement.closest('.start-page-column')?.dataset.column").to_i
  end

  def focus_inside_grid?
    evaluate_script("!!document.activeElement.closest('#start_page_grid')")
  end

  def sign_in_as(user)
    visit new_session_path
    fill_in "email", with: user[:email]
    fill_in "password", with: "password123"
    click_button "Sign in"
    # Waits on the page it lands on rather than the absence of the one it left.
    # A negative assertion makes Capybara re-check visibility on nodes it has
    # already found, and a node the navigation swapped out raises "Node with
    # given id does not belong to the document" rather than simply missing.
    # Generous wait: logins get slow when the whole suite runs in parallel.
    assert_selector "main.start-page", wait: 10
  end
end
