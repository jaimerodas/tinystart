require "application_system_test_case"

# Reordering lives in start_page_keyboard_test.rb. Dragging still cannot be
# driven here — HTML5 drag events do not respond to Selenium's synthetic mouse
# events — but the keyboard reaches the same endpoints through the same
# lib/start_page_moves, so the reorder behaviour is covered end to end. What is
# left unverified by machine is narrow: the browser's own decision to begin a
# drag from a mousedown on a `draggable` handle.
class StartPageIntegrationTest < ApplicationSystemTestCase
  def setup
    @user = users(:one)
    sign_in_as(@user)
  end

  # There is nothing to create any more — the grid is there from signup, and the
  # only thing to configure is how wide it is. That happens in the editor's
  # toolbar, with no submit button: picking the value is the whole interaction.
  test "user can change the column count from the editor" do
    visit edit_start_path
    assert_selector ".start-page-grid[data-columns='3']"

    select "5", from: "Columns"

    assert_selector ".start-page-grid[data-columns='5']"
    assert_selector "#column_count select option[selected]", text: "5", visible: :all

    visit root_path
    assert_selector ".start-page-grid[data-columns='5']"
  end

  # The whole reason the control moved: a shrink can be refused, and the group
  # the refusal names is only on screen here.
  test "a shrink that would strand a group is refused on the page that shows it" do
    @user.start_page_groups.create!(name: "Reading", column: 3, position: 0)

    visit edit_start_path
    select "1", from: "Columns"

    assert_selector "#start_page_notice", text: /that would hide "Reading"/
    assert_selector ".start-page-grid[data-columns='3']"
    # A refusal redraws rather than only reporting, so the select goes back too.
    assert_selector "#column_count select option[selected]", text: "3", visible: :all
    assert_equal 3, @user.reload.columns
  end

  # The whole editing loop in one pass, each step done where the thing lives:
  # a group from the foot of its column, tiles from the foot of the group.
  # Every write swaps a node in place, so these wait on rendered state and the
  # database rather than on a flash.
  test "user can add a group, add tiles, edit them and delete" do
    visit edit_start_path

    within("#new_group_column_1") do
      click_button "Add group"
      fill_in "Group name", with: "Daily"
      click_button "Add"
    end

    assert_selector "#column_1 .group-name", text: "Daily"
    group = @user.start_page_groups.find_by(name: "Daily")
    assert_equal 1, group.column

    within("#new_item_group_#{group.id}") do
      click_button "Add link"
      fill_in "Title", with: "GitHub"
      fill_in "URL", with: "https://github.com"
      click_button "Add"
    end

    assert_selector "#group_#{group.id} .item-title", text: "GitHub"

    # The add form comes back open, so a second link needs no second click.
    within("#new_item_group_#{group.id}") do
      assert_selector ".inline-form"
      fill_in "Title", with: "Apple"
      fill_in "URL", with: "https://apple.com"
      click_button "Add"
    end

    # Capybara retries the DOM, not the database, so wait on the render first.
    assert_selector "#group_#{group.id} .item-title", text: "Apple"
    assert_equal [ "GitHub", "Apple" ], group.ordered_items.map(&:title)

    # Editing a tile opens the same form that added it, over its own row.
    github = group.start_page_items.find_by(title: "GitHub")
    within("#item_#{github.id}") do
      click_button "Edit tile"
      fill_in "Title", with: "GitHub Home"
      click_button "Save"
    end

    assert_selector "#item_#{github.id} .item-title", text: "GitHub Home"
    assert_equal "GitHub Home", github.reload.title

    within("#group_#{group.id} .group-heading") do
      click_button "Rename group"
      fill_in "Group name", with: "Every day"
      click_button "Save"
    end

    assert_selector "#group_#{group.id} .group-name", text: "Every day"

    # Reordering has a file of its own — see the note at the top.
    accept_confirm do
      within("#item_#{github.id}") { click_button "Remove tile" }
    end

    assert_no_selector "#item_#{github.id}"
    assert_equal [ "Apple" ], group.reload.ordered_items.map(&:title)
    assert_equal [ 0 ], group.ordered_items.map(&:position)
  end

  # A rejected save has to leave the form where it was, still holding what was
  # typed, or the message has nothing to point at.
  test "a rejected tile keeps its form open with the error and the typed values" do
    group = @user.start_page_groups.create!(name: "Tools", column: 1, position: 0)
    group.start_page_items.create!(url: "https://github.com", title: "GitHub", position: 0)

    visit edit_start_path

    within("#new_item_group_#{group.id}") do
      click_button "Add link"
      fill_in "Title", with: "Duplicate"
      fill_in "URL", with: "https://github.com"
      click_button "Add"

      assert_selector ".form-errors", text: "Url has already been taken"
      assert_field "Title", with: "Duplicate"
      assert_selector ".inline-form"
    end

    assert_equal 1, group.reload.start_page_items.count
  end

  # The form keeps what was typed so the error has something to point at, but
  # the row behind it describes what is actually saved — and Cancel has to
  # discard the refused values, not adopt them.
  test "a rejected edit leaves the row and a reopened form showing the saved values" do
    group = @user.start_page_groups.create!(name: "Tools", column: 1, position: 0)
    item = group.start_page_items.create!(url: "https://github.com", title: "GitHub", position: 0)

    visit edit_start_path

    within("#item_#{item.id}") do
      click_button "Edit tile"
      fill_in "Title", with: "Renamed"
      # Passes the browser's own url validation, fails the model's
      fill_in "URL", with: "ftp://example.com"
      click_button "Save"

      assert_selector ".form-errors", text: "Url must be a valid URL"
      assert_field "Title", with: "Renamed"
      # The row behind the open form still reports the saved title
      assert_selector ".item-title", text: "GitHub", visible: :all

      click_button "Cancel"
      assert_no_selector ".form-errors"

      click_button "Edit tile"
      assert_field "Title", with: "GitHub"
      assert_field "URL", with: "https://github.com"
      assert_no_selector ".form-errors"
    end

    assert_equal "GitHub", item.reload.title
    assert_equal "https://github.com", item.url
  end

  # Cancel closes the form and throws the edit away rather than saving it.
  test "cancelling an edit leaves the tile alone" do
    group = @user.start_page_groups.create!(name: "Tools", column: 1, position: 0)
    item = group.start_page_items.create!(url: "https://github.com", title: "GitHub", position: 0)

    visit edit_start_path

    within("#item_#{item.id}") do
      click_button "Edit tile"
      fill_in "Title", with: "Something else"
      click_button "Cancel"

      assert_no_selector ".inline-form", visible: true
      assert_selector ".item-title", text: "GitHub"
    end

    assert_equal "GitHub", item.reload.title
  end

  test "command bar filters the tiles on the page" do
    tiles_for_filtering

    visit root_path

    assert_selector ".command-bar input[autofocus]"
    command_input = find(".command-bar input")

    command_input.fill_in(with: "a")

    within(".command-bar-suggestions") do
      assert_text "Amazon Shopping"
      assert_text "Apple"
      assert_no_text "GitHub"
    end

    command_input.fill_in(with: "")
    assert_no_selector ".command-bar-suggestions", visible: true

    # Matching is case-insensitive
    command_input.fill_in(with: "APPLE")

    within(".command-bar-suggestions") do
      assert_text "Apple"
      assert_no_text "Amazon"
      assert_no_text "GitHub"
    end
  end

  # Nothing to federate to means no "All Links" at all — not a header that
  # flashes "Searching..." and then quietly empties itself.
  test "command bar offers no All Links section without a connection" do
    tiles_for_filtering

    visit root_path
    find(".command-bar input").fill_in(with: "a")

    # The local results and the All Links header used to render in the same tick,
    # so these have to be checked without waiting — a patient assertion passes
    # either way once /search.json answers with an empty list.
    within(".command-bar-suggestions") { assert_text "Amazon Shopping" }
    assert_selector ".command-bar-section-header", count: 1, wait: 0
    assert_no_selector ".command-bar-searching", wait: 0
  end

  # A rejected token is worth saying out loud, but retrying it isn't.
  test "command bar says so instead of searching once the token was rejected" do
    tiles_for_filtering
    connection = @user.create_connection!(base_url: "https://links.example.com", token: "mine")
    connection.record_failure!("links.example.com rejected the token")

    visit root_path
    find(".command-bar input").fill_in(with: "a")

    within(".command-bar-suggestions") do
      assert_text "Amazon Shopping"
      assert_selector ".command-bar-notice", text: "links.example.com search disconnected — reconnect in Settings.", wait: 0
    end
    assert_selector ".command-bar-section-header", count: 1, wait: 0
    assert_no_selector ".command-bar-searching", wait: 0
  end

  test "clicking a tile records a visit" do
    host = "http://#{page.server.host}:#{page.server.port}"
    group = @user.start_page_groups.create!(name: "Tools", column: 1, position: 0)
    # Point at the in-app health route so the same-tab navigation stays
    # same-origin and resolves instantly (no external load).
    item = group.start_page_items.create!(url: "#{host}/up", title: "Health Check", position: 0)

    visit root_path
    assert_equal 0, item.reload.visit_count

    click_link "Health Check"

    assert_visit_recorded(item)
  end

  test "selecting a command bar suggestion records a visit" do
    host = "http://#{page.server.host}:#{page.server.port}"
    group = @user.start_page_groups.create!(name: "Shopping", column: 1, position: 0)
    item = group.start_page_items.create!(url: "#{host}/up", title: "Apple", position: 0)

    visit root_path
    command_input = find(".command-bar input")
    command_input.fill_in(with: "Apple")

    within(".command-bar-suggestions") { assert_text "Apple" }

    command_input.send_keys(:enter)

    assert_visit_recorded(item)
  end

  private

  def tiles_for_filtering
    group = @user.start_page_groups.create!(name: "Shopping", column: 1, position: 0)
    group.start_page_items.create!(url: "https://amazon.com", title: "Amazon Shopping", position: 0)
    group.start_page_items.create!(url: "https://apple.com", title: "Apple", position: 1)
    group.start_page_items.create!(url: "https://github.com", title: "GitHub", position: 2)
    group
  end

  # Capybara retries DOM assertions, not the database, so poll the item until
  # the fire-and-forget visit POST lands (or the wait window elapses).
  def assert_visit_recorded(item, count: 1)
    deadline = Time.now + Capybara.default_max_wait_time
    sleep 0.05 until item.reload.visit_count >= count || Time.now > deadline
    assert_equal count, item.reload.visit_count
  end

  def dismiss_flash
    find(".flash-overlay").click
    assert_no_selector ".flash-overlay"
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
