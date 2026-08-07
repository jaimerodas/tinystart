require "test_helper"

class SettingsControllerTest < ActionDispatch::IntegrationTest
  def setup
    @user = users(:one)
    sign_in_as(@user)
  end

  test "should show the start page summary" do
    group = @user.start_page_groups.create!(name: "Work", column: 1, position: 0)
    group.start_page_items.create!(url: "https://example.com", title: "Example", position: 0)

    get settings_path

    assert_response :success
    assert_select "#start-page", /Groups:\s*1/
    assert_select "#start-page", /Total Links:\s*1/
  end

  # The column count used to live on its own start_pages row; it is a user
  # preference now, updated alongside theme and colour.
  test "should update the start page column count" do
    patch settings_path, params: { user: { columns: 5 } }

    assert_redirected_to settings_path
    assert_equal 5, @user.reload.columns
  end

  test "should not update to a column count outside the allowed range" do
    patch settings_path, params: { user: { columns: 9 } }

    assert_redirected_to settings_path
    assert_equal "Failed to update settings.", flash[:alert]
    assert_equal 3, @user.reload.columns
  end

  test "should require authentication" do
    sign_out

    get settings_path
    assert_redirected_to new_session_path
  end

  private

  def sign_in_as(user)
    post session_url, params: { email: user[:email], password: "password123" }
  end

  def sign_out
    delete session_path
  end
end
