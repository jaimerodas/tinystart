require "application_system_test_case"

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

  # The whole editing loop in one pass: make a group, put a tile in it, move the
  # tile, then take it away again.
  test "user can add a group, add a tile, reorder tiles and delete one" do
    visit edit_start_path

    fill_in "Group Name", with: "Daily"
    click_button "Add Group"
    assert_text "Group created successfully"
    dismiss_flash
    assert_text "Daily"

    fill_in "Title", with: "GitHub"
    fill_in "URL", with: "https://github.com"
    click_button "Add Tile"
    assert_text "Tile added"
    dismiss_flash

    fill_in "Title", with: "Apple"
    fill_in "URL", with: "https://apple.com"
    click_button "Add Tile"
    assert_text "Tile added"
    dismiss_flash

    group = @user.start_page_groups.find_by(name: "Daily")
    assert_equal [ "GitHub", "Apple" ], group.ordered_items.map(&:title)

    # Move the second tile up. The drag handles need real pointer drags, so the
    # move buttons are what a system test can drive; both hit the same endpoint.
    within(".start-page-item", text: "Apple") { click_button "Move item up" }

    # The grid is swapped in place, so wait on the rendered order rather than a flash.
    assert_selector ".start-page-item:first-of-type", text: "Apple"
    assert_equal [ "Apple", "GitHub" ], group.reload.ordered_items.map(&:title)

    accept_confirm do
      within(".start-page-item", text: "GitHub") { click_button "Remove tile" }
    end
    assert_text "Tile removed"

    assert_equal [ "Apple" ], group.reload.ordered_items.map(&:title)
    assert_equal [ 0 ], group.ordered_items.map(&:position)
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
