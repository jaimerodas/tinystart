require "test_helper"

class StartPagesControllerTest < ActionDispatch::IntegrationTest
  def setup
    @user = users(:one)
    sign_in_as(@user)
  end

  # There is no start page to create any more: every user has one from signup.
  test "should show the start page without any setup" do
    get root_path

    assert_response :success
    assert_select ".start-page-grid[data-columns='3']"
  end

  # The page has one URL. /start is still the PATCH target and the prefix every
  # group and item route hangs off, but it is not somewhere you can go.
  test "should not serve the start page at /start as well" do
    get "/start"

    assert_response :not_found
  end

  # A blank grid is indistinguishable from a broken one, so say which it is.
  test "should tell a user with no tiles how to add some" do
    get root_path

    assert_response :success
    assert_select ".start-page-empty", /No links added yet/
    assert_select ".start-page-empty a[href=?]", edit_start_path, "edit the page"
  end

  test "should not show the empty notice once a tile exists" do
    group = @user.start_page_groups.create!(name: "Work", column: 1, position: 0)
    group.start_page_items.create!(url: "https://example.com", title: "Example", position: 0)

    get root_path

    assert_select ".start-page-empty", false
  end

  # Groups without tiles still leave the page blank, so the notice has to stay.
  test "should still show the empty notice for a group with no tiles" do
    @user.start_page_groups.create!(name: "Work", column: 1, position: 0)

    get root_path

    assert_select ".start-page-empty", /No links added yet/
  end

  test "should lay the grid out with the user's column count" do
    @user.update!(columns: 5)

    get root_path

    assert_response :success
    assert_select ".start-page-grid[data-columns='5']"
    assert_select ".start-page-column", 5
  end

  test "should include command bar with links data" do
    group = @user.start_page_groups.create!(name: "Test Group", column: 1, position: 0)
    group.start_page_items.create!(url: "https://amazon.com", title: "Amazon", position: 0)
    group.start_page_items.create!(url: "https://github.com", title: "GitHub", position: 1)

    get root_path
    assert_response :success

    # Check command bar elements exist
    assert_select ".command-bar"
    assert_select ".command-bar input[data-command-bar-target='input']"
    assert_select ".command-bar-suggestions[data-command-bar-target='suggestions']"

    # Check that the main element has the command bar controller and links data
    assert_select "main[data-controller~='command-bar']"
    assert_select "main[data-command-bar-links-value]" do |elements|
      links_json = elements.first["data-command-bar-links-value"]
      links_data = JSON.parse(links_json)
      assert_equal 2, links_data.length
      assert_equal "Amazon", links_data[0]["title"]
      assert_equal "https://amazon.com", links_data[0]["url"]
    end
  end

  # The command bar can't tell "not connected" from "no matches" on its own, so
  # the page has to hand it the state up front.
  test "should tell the command bar federation is off without a connection" do
    get root_path

    assert_select "main[data-command-bar-federation-value='off']"
  end

  # The federated section is named after the host it came from, so the page has
  # to hand that over too.
  test "should tell the command bar federation is active with a connection" do
    @user.create_connection!(base_url: "https://links.example.com", token: "mine")

    get root_path

    assert_select "main[data-command-bar-federation-value='active']"
    assert_select "main[data-command-bar-source-value='links.example.com']"
  end

  test "should tell the command bar to stop searching once the token was rejected" do
    connection = @user.create_connection!(base_url: "https://links.example.com", token: "mine")
    connection.record_failure!("links.example.com rejected the token")

    get root_path

    assert_select "main[data-command-bar-federation-value='reconnect']"
    assert_select ".search-disconnected", /Search of links\.example\.com is disconnected/
    assert_select ".search-disconnected a[href=?]", settings_connections_path, "Reconnect"
  end

  # One person's tiles must never surface in another person's grid or command bar.
  test "should only show the signed-in user's tiles" do
    mine = @user.start_page_groups.create!(name: "Mine", column: 1, position: 0)
    mine.start_page_items.create!(url: "https://mine.example.com", title: "Mine", position: 0)

    theirs = users(:two).start_page_groups.create!(name: "Theirs", column: 1, position: 0)
    theirs.start_page_items.create!(url: "https://theirs.example.com", title: "Theirs", position: 0)

    get root_path

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

    get root_path
    assert_redirected_to new_session_path
  end

  # A lapsed token must be visible: silent federated failure is indistinguishable
  # from an empty archive.
  test "shows a reconnect notice when the token was rejected" do
    Connection.create!(user: @user, base_url: "https://links.example.com", token: "t")
      .record_failure!("links.example.com rejected the token")

    get root_path

    assert_select ".search-disconnected", /disconnected/i
  end

  test "shows no reconnect notice while the connection is healthy" do
    Connection.create!(user: @user, base_url: "https://links.example.com", token: "t")

    get root_path

    assert_select ".search-disconnected", false
  end

  test "shows no reconnect notice when the app was never connected" do
    get root_path

    assert_select ".search-disconnected", false
  end

  # --- the column count ---
  #
  # It used to be a field on the Preferences form. It lives in the editor's
  # toolbar now, so a refused shrink can answer on the page that is showing the
  # group it names.

  # The default is one column. If 1 were not on offer the browser would
  # preselect the first option, and a user could never get back to one.
  test "should offer every valid column count, with the current one selected" do
    fresh = User.create!(email: "fresh@example.com", password: "password123", approved: true)
    post session_url, params: { email: fresh.email, password: "password123" }

    get edit_start_path

    assert_equal 1, fresh.columns
    assert_select "#column_count select[name='user[columns]']" do
      assert_select "option", 6
      assert_select "option[value='1'][selected='selected']"
    end
  end

  test "should update the column count and send the editor back for a redraw" do
    patch start_path, params: { user: { columns: 5 } }

    assert_redirected_to edit_start_path
    assert_equal 5, @user.reload.columns
  end

  # A refusal has to redraw, not just report: the select is already showing the
  # value the database rejected, so it has to be sent back too.
  test "should refuse a count outside the range and reset the select" do
    patch start_path, params: { user: { columns: 9 } }, as: :turbo_stream

    assert_response :unprocessable_content
    assert_equal 3, @user.reload.columns
    assert_match "start_page_notice", response.body
    assert_match "column_count", response.body
    assert_match %r{<option selected="selected" value="3">}, response.body
  end

  # Rails wraps the fields of a model carrying errors in a .field_with_errors
  # div, which is a block element — it would break the one-line toolbar apart.
  # The refusal is spoken by the notice, so the select comes back plain.
  test "should send the select back without error wrappers" do
    patch start_path, params: { user: { columns: 9 } }, as: :turbo_stream

    assert_no_match(/field_with_errors/, response.body)
  end

  # There is no stream to apply without Turbo, so the refusal has to reach the
  # user some other way rather than as raw <turbo-stream> markup on screen.
  test "should refuse in a flash when the request is not a turbo stream" do
    patch start_path, params: { user: { columns: 9 } }

    assert_redirected_to edit_start_path
    assert_match(/less than or equal to 6/, flash[:alert])
    assert_equal 3, @user.reload.columns
  end

  # Saying only "failed" would leave you re-picking the same value forever.
  test "should say which group blocks a shrink" do
    @user.start_page_groups.create!(name: "Reading", column: 3, position: 0)

    patch start_path, params: { user: { columns: 1 } }, as: :turbo_stream

    assert_response :unprocessable_content
    assert_match(/that would hide &quot;Reading&quot;/, response.body)
    assert_equal 3, @user.reload.columns
  end

  test "should not let one user set another's column count" do
    other = users(:two)

    patch start_path, params: { user: { columns: 6 } }

    assert_equal 3, other.reload.columns
    assert_equal 6, @user.reload.columns
  end

  private

  def sign_in_as(user)
    post session_url, params: { email: user[:email], password: "password123" }
  end

  def sign_out
    delete session_path
  end
end
