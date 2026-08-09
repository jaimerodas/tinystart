require "test_helper"

class SettingsControllerTest < ActionDispatch::IntegrationTest
  def setup
    @user = users(:one)
    sign_in_as(@user)
  end

  # Links lead: it is the number worth glancing at, and the one that grows.
  test "should show the link and group counts, links first" do
    group = @user.start_page_groups.create!(name: "Work", column: 1, position: 0)
    group.start_page_items.create!(url: "https://example.com", title: "Example", position: 0)
    group.start_page_items.create!(url: "https://example.org", title: "Other", position: 1)

    get settings_path

    assert_response :success
    assert_select "#start-page-stats .stat" do |stats|
      assert_equal 2, stats.length
      assert_equal [ "2", "links" ], stat_text(stats[0])
      assert_equal [ "1", "group" ], stat_text(stats[1])
    end
  end

  test "should say member since with an absolute date and a machine-readable time" do
    get settings_path

    assert_select "#user-details time[datetime=?]", @user.created_at.iso8601
  end

  # The column count moved to /start/edit, where the groups a shrink would
  # strand are actually on screen. This page must not quietly keep writing it,
  # or the two controls drift apart.
  test "should not offer or accept a column count" do
    get settings_path

    assert_select "select[name='user[columns]']", false

    patch settings_path, params: { user: { columns: 5, theme_preference: "dark" } }

    assert_redirected_to settings_path
    assert_equal 3, @user.reload.columns
    assert_equal "dark", @user.theme_preference
  end

  test "should update theme and color" do
    patch settings_path, params: { user: { theme_preference: "light", color_preference: "teal" } }

    assert_redirected_to settings_path
    assert_equal "light", @user.reload.theme_preference
    assert_equal "teal", @user.color_preference
  end

  test "should not update to an invalid theme" do
    patch settings_path, params: { user: { theme_preference: "neon" } }

    assert_redirected_to settings_path
    assert_match(/Failed to update settings/, flash[:alert])
    assert_equal "system", @user.reload.theme_preference
  end

  test "should require authentication" do
    sign_out

    get settings_path
    assert_redirected_to new_session_path
  end

  private

  # [value, label] as a reader would see them, whitespace collapsed.
  def stat_text(stat)
    stat.children.select(&:element?).map { |node| node.text.strip }
  end

  def sign_in_as(user)
    post session_url, params: { email: user[:email], password: "password123" }
  end

  def sign_out
    delete session_path
  end
end
