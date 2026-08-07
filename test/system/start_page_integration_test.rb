require "application_system_test_case"

class StartPageIntegrationTest < ApplicationSystemTestCase
  def setup
    @user = users(:one)
    sign_in_as(@user)
  end

  test "user can create a start page from settings" do
    visit start_path

    # With no start page yet, the page sends you to settings to make one.
    assert_current_path settings_start_page_path
    assert_text "Create your start page to get started"
    dismiss_flash
    assert_text "Start Page"

    assert_field "Name", with: "Start"

    fill_in "Name", with: "My Dashboard"
    select "4", from: "Columns"
    click_button "Create Start Page"

    assert_current_path settings_start_page_path
    assert_text "Start page created successfully"
    assert_text "My Dashboard"

    visit start_path
    assert_selector ".start-page-grid[data-columns='4']"
  end

  # The whole editing loop in one pass: make a group, put a tile in it, move the
  # tile, then take it away again.
  test "user can add a group, add a tile, reorder tiles and delete one" do
    StartPage.create!(user: @user, name: "My Start Page", columns: 3)

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

    group = @user.start_page.start_page_groups.find_by(name: "Daily")
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
    start_page = StartPage.create!(user: @user, name: "My Start Page", columns: 3)
    group = start_page.start_page_groups.create!(name: "Shopping", column: 1, position: 0)
    group.start_page_items.create!(url: "https://amazon.com", title: "Amazon Shopping", position: 0)
    group.start_page_items.create!(url: "https://apple.com", title: "Apple", position: 1)
    group.start_page_items.create!(url: "https://github.com", title: "GitHub", position: 2)

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

  test "clicking a tile records a visit" do
    host = "http://#{page.server.host}:#{page.server.port}"
    start_page = StartPage.create!(user: @user, name: "My Start Page", columns: 3)
    group = start_page.start_page_groups.create!(name: "Tools", column: 1, position: 0)
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
    start_page = StartPage.create!(user: @user, name: "My Start Page", columns: 3)
    group = start_page.start_page_groups.create!(name: "Shopping", column: 1, position: 0)
    item = group.start_page_items.create!(url: "#{host}/up", title: "Apple", position: 0)

    visit start_path
    command_input = find(".command-bar input")
    command_input.fill_in(with: "Apple")

    within(".command-bar-suggestions") { assert_text "Apple" }

    command_input.send_keys(:enter)

    assert_visit_recorded(item)
  end

  private

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
    # Root is the start page, which bounces to settings when none exists yet —
    # so just assert we're off the login screen. Generous wait: logins get slow
    # when the whole suite runs in parallel.
    assert_no_selector "#login", wait: 10
  end
end
