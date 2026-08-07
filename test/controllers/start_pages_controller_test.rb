require "test_helper"

class StartPagesControllerTest < ActionDispatch::IntegrationTest
  def setup
    @user = users(:one)
    sign_in_as(@user)
  end

  # There is no start page to create any more: every user has one from signup.
  test "should show the start page without any setup" do
    get start_path

    assert_response :success
    assert_select ".start-page-grid[data-columns='3']"
  end

  # A blank grid is indistinguishable from a broken one, so say which it is.
  test "should tell a user with no tiles how to add some" do
    get start_path

    assert_response :success
    assert_select ".start-page-empty", /No links added yet/
    assert_select ".start-page-empty a[href=?]", edit_start_path, "edit the page"
  end

  test "should not show the empty notice once a tile exists" do
    group = @user.start_page_groups.create!(name: "Work", column: 1, position: 0)
    group.start_page_items.create!(url: "https://example.com", title: "Example", position: 0)

    get start_path

    assert_select ".start-page-empty", false
  end

  # Groups without tiles still leave the page blank, so the notice has to stay.
  test "should still show the empty notice for a group with no tiles" do
    @user.start_page_groups.create!(name: "Work", column: 1, position: 0)

    get start_path

    assert_select ".start-page-empty", /No links added yet/
  end

  test "should lay the grid out with the user's column count" do
    @user.update!(columns: 5)

    get start_path

    assert_response :success
    assert_select ".start-page-grid[data-columns='5']"
    assert_select ".start-page-column", 5
  end

  test "should include command bar with links data" do
    group = @user.start_page_groups.create!(name: "Test Group", column: 1, position: 0)
    group.start_page_items.create!(url: "https://amazon.com", title: "Amazon", position: 0)
    group.start_page_items.create!(url: "https://github.com", title: "GitHub", position: 1)

    get start_path
    assert_response :success

    # Check command bar elements exist
    assert_select ".command-bar"
    assert_select ".command-bar input[data-command-bar-target='input']"
    assert_select ".command-bar-suggestions[data-command-bar-target='suggestions']"

    # Check that the main element has the command bar controller and links data
    assert_select "main[data-controller='command-bar']"
    assert_select "main[data-command-bar-links-value]" do |elements|
      links_json = elements.first["data-command-bar-links-value"]
      links_data = JSON.parse(links_json)
      assert_equal 2, links_data.length
      assert_equal "Amazon", links_data[0]["title"]
      assert_equal "https://amazon.com", links_data[0]["url"]
    end
  end

  # One person's tiles must never surface in another person's grid or command bar.
  test "should only show the signed-in user's tiles" do
    mine = @user.start_page_groups.create!(name: "Mine", column: 1, position: 0)
    mine.start_page_items.create!(url: "https://mine.example.com", title: "Mine", position: 0)

    theirs = users(:two).start_page_groups.create!(name: "Theirs", column: 1, position: 0)
    theirs.start_page_items.create!(url: "https://theirs.example.com", title: "Theirs", position: 0)

    get start_path

    assert_select ".start-page-grid", /Mine/
    assert_select ".start-page-grid", { text: /Theirs/, count: 0 }
    assert_select "main[data-command-bar-links-value]" do |elements|
      titles = JSON.parse(elements.first["data-command-bar-links-value"]).map { |l| l["title"] }
      assert_equal [ "Mine" ], titles
    end
  end

  test "should get edit" do
    get edit_start_path

    assert_response :success
    assert_select "form"
  end

  test "should require authentication" do
    sign_out

    get start_path
    assert_redirected_to new_session_path
  end

  # A lapsed token must be visible: silent federated failure is indistinguishable
  # from an empty archive.
  test "shows a reconnect notice when the tinylinks token was rejected" do
    TinylinksConnection.create!(user: @user, base_url: "https://links.example.com", token: "t")
      .record_failure!("tinylinks rejected the token")

    get start_path

    assert_select ".tinylinks-disconnected", /disconnected/i
  end

  test "shows no reconnect notice while the connection is healthy" do
    TinylinksConnection.create!(user: @user, base_url: "https://links.example.com", token: "t")

    get start_path

    assert_select ".tinylinks-disconnected", false
  end

  test "shows no reconnect notice when the app was never connected" do
    get start_path

    assert_select ".tinylinks-disconnected", false
  end

  private

  def sign_in_as(user)
    post session_url, params: { email: user[:email], password: "password123" }
  end

  def sign_out
    delete session_path
  end
end
