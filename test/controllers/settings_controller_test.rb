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

  # The default is one column, and this select shares the Preferences form with
  # theme and colour. If 1 is not on offer the browser preselects the first
  # option, so saving a theme change silently widens someone's grid — and they
  # can never get back to one column.
  test "should offer every valid column count, with the current one selected" do
    fresh = User.create!(email: "fresh@example.com", password: "password123", approved: true)
    post session_url, params: { email: fresh.email, password: "password123" }

    get settings_path

    assert_equal 1, fresh.columns
    assert_select "select[name='user[columns]']" do
      assert_select "option", 6
      assert_select "option[value='1'][selected='selected']"
    end
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
    assert_match(/Failed to update settings/, flash[:alert])
    assert_equal 3, @user.reload.columns
  end

  # Saying only "failed" would leave you re-picking the same value forever.
  test "should say which groups block a shrink" do
    @user.start_page_groups.create!(name: "Reading", column: 3, position: 0)

    patch settings_path, params: { user: { columns: 1 } }

    assert_redirected_to settings_path
    assert_match(/that would hide "Reading"/, flash[:alert])
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
