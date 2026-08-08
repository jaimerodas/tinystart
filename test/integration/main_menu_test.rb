require "test_helper"

class MainMenuTest < ActionDispatch::IntegrationTest
  setup do
    @user = users(:one)
    post session_url, params: { email: @user[:email], password: "password123" }
  end

  # Every page carrying this menu is a settings page, so the old "Settings"
  # link only ever pointed at where you already were. The way out is the way
  # back to the start page.
  test "offers a way back to the start page instead of a link to settings" do
    get settings_path

    assert_select "header nav a[href=?]", root_path, text: "Start"
    assert_select "header nav a[href=?]", settings_path, count: 0
  end

  test "still offers a way to log out" do
    get settings_path

    assert_select "header nav form[action=?] button", session_path, text: "Log out"
  end

  # Two items never need compacting, so the labels are the whole button at
  # every width — there is no icon-only mobile rendering to fall back to.
  test "labels every menu item with text" do
    get settings_path

    assert_select "header nav svg", count: 0
  end
end
