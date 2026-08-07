require "test_helper"

class StartPagesControllerTest < ActionDispatch::IntegrationTest
  def setup
    @user = users(:one)
    sign_in_as(@user)
  end

  test "should redirect to start page settings if no start page exists" do
    get start_path
    assert_redirected_to settings_start_page_path
    assert_equal "Create your start page to get started.", flash[:notice]
  end

  test "should show start page if exists" do
    start_page = StartPage.create!(user: @user, name: "My Start Page", columns: 3)

    get start_path
    assert_response :success
    assert_select ".start-page-grid[data-columns='3']"
  end

  test "should include command bar with links data" do
    start_page = StartPage.create!(user: @user, name: "My Start Page", columns: 3)

    group = start_page.start_page_groups.create!(name: "Test Group", column: 1, position: 0)
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


  test "should create start page" do
    assert_difference("StartPage.count") do
      post start_path, params: {
        start_page: {
          name: "Test Start Page",
          columns: 4
        }
      }
    end

    assert_redirected_to settings_start_page_path
    assert_equal "Start page created successfully.", flash[:notice]

    start_page = StartPage.find_by(user: @user)
    assert_equal "Test Start Page", start_page.name
    assert_equal 4, start_page.columns
  end

  test "should not create invalid start page" do
    assert_no_difference("StartPage.count") do
      post start_path, params: {
        start_page: {
          name: "",
          columns: 10
        }
      }
    end

    assert_redirected_to settings_start_page_path
    assert_match /Failed to create start page/, flash[:alert]
  end

  test "should get edit" do
    start_page = StartPage.create!(user: @user, name: "My Start Page", columns: 3)

    get edit_start_path
    assert_response :success
    assert_select "form"
  end

  test "should update start page" do
    start_page = StartPage.create!(user: @user, name: "My Start Page", columns: 3)

    patch start_path, params: {
      start_page: {
        name: "Updated Start Page",
        columns: 5
      }
    }

    assert_redirected_to settings_start_page_path
    assert_equal "Start page updated successfully.", flash[:notice]

    start_page.reload
    assert_equal "Updated Start Page", start_page.name
    assert_equal 5, start_page.columns
  end

  test "should not update with invalid data" do
    start_page = StartPage.create!(user: @user, name: "My Start Page", columns: 3)

    patch start_path, params: {
      start_page: {
        name: "",
        columns: 0
      }
    }

    assert_response :success
    assert_select "form"

    start_page.reload
    assert_equal "My Start Page", start_page.name
    assert_equal 3, start_page.columns
  end

  test "should require authentication" do
    sign_out

    get start_path
    assert_redirected_to new_session_path
  end

  private

  def sign_in_as(user)
    post session_url, params: { email: user[:email], password: "password123" }
  end

  def sign_out
    delete session_path
  end
  # A lapsed token must be visible: silent federated failure is indistinguishable
  # from an empty archive.
  test "shows a reconnect notice when the tinylinks token was rejected" do
    StartPage.create!(user: @user, name: "My Start Page", columns: 3)
    TinylinksConnection.create!(base_url: "https://links.example.com", token: "t")
      .record_failure!("tinylinks rejected the token")

    get start_path

    assert_select ".tinylinks-disconnected", /disconnected/i
  end

  test "shows no reconnect notice while the connection is healthy" do
    StartPage.create!(user: @user, name: "My Start Page", columns: 3)
    TinylinksConnection.create!(base_url: "https://links.example.com", token: "t")

    get start_path

    assert_select ".tinylinks-disconnected", false
  end

  test "shows no reconnect notice when the app was never connected" do
    StartPage.create!(user: @user, name: "My Start Page", columns: 3)

    get start_path

    assert_select ".tinylinks-disconnected", false
  end
end
