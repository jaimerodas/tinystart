require "application_system_test_case"

# ⚠️ Reordering is not covered here, and cannot be. Dragging is the only way to
# reorder a tile or a group since the move buttons were removed, and HTML5 drag
# events do not respond to Selenium's synthetic mouse events. What the drag
# posts to — `move` on both controllers, `move_item_to_position` and
# `move_to_column` — is covered by the controller and model tests; the drag
# handlers on top of that are verified by hand in a real browser.
class StartPageIntegrationTest < ApplicationSystemTestCase
  def setup
    @user = users(:one)
    sign_in_as(@user)
  end

  # There is nothing to create any more — the grid is there from signup, and the
  # only thing to configure is how wide it is.
  test "user can change the start page column count from settings" do
    visit start_path
    assert_selector ".start-page-grid[data-columns='3']"

    visit settings_path
    select "5", from: "Start Page Columns"
    click_button "Update Preferences"

    assert_current_path settings_path
    assert_text "Settings updated successfully"

    visit start_path
    assert_selector ".start-page-grid[data-columns='5']"
  end

  # The whole editing loop in one pass, each step done where the thing lives:
  # a group from the foot of its column, tiles from the foot of the group.
  # Every write swaps a node in place, so these wait on rendered state and the
  # database rather than on a flash.
  test "user can add a group, add tiles, edit them, reorder and delete" do
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

    # Reordering is drag-only now, and HTML5 drag events do not survive
    # Selenium — the endpoint it posts to is covered in the controller and model
    # tests instead. See the note at the top of this file.
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

    visit start_path

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
  test "command bar offers no All Links section without a tinylinks connection" do
    tiles_for_filtering

    visit start_path
    find(".command-bar input").fill_in(with: "a")

    # The local results and the All Links header used to render in the same tick,
    # so these have to be checked without waiting — a patient assertion passes
    # either way once /search.json answers with an empty list.
    within(".command-bar-suggestions") { assert_text "Amazon Shopping" }
    assert_selector ".command-bar-section-header", count: 1, wait: 0
    assert_no_selector ".command-bar-searching", wait: 0
  end

  # A rejected token is worth saying out loud, but retrying it isn't.
  test "command bar says so instead of searching once the tinylinks token was rejected" do
    tiles_for_filtering
    connection = @user.create_tinylinks_connection!(base_url: "https://links.example.com", token: "mine")
    connection.record_failure!("tinylinks rejected the token")

    visit start_path
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

    visit start_path
    assert_equal 0, item.reload.visit_count

    click_link "Health Check"

    assert_visit_recorded(item)
  end

  test "selecting a command bar suggestion records a visit" do
    host = "http://#{page.server.host}:#{page.server.port}"
    group = @user.start_page_groups.create!(name: "Shopping", column: 1, position: 0)
    item = group.start_page_items.create!(url: "#{host}/up", title: "Apple", position: 0)

    visit start_path
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
    # Generous wait: logins get slow when the whole suite runs in parallel.
    assert_no_selector "#login", wait: 10
  end
end
