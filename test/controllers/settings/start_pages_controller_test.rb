require "test_helper"

class Settings::StartPagesControllerTest < ActionDispatch::IntegrationTest
  def setup
    @user = users(:one)
    post session_url, params: { email: @user[:email], password: "password123" }
  end

  test "should show start page section when no start page exists" do
    get settings_start_page_url
    assert_response :success
    assert_select "h2", "Start Page"
    assert_select "p", text: /Create a customized start page/
    assert_select "form[action='#{start_path}']"
    assert_select "input[name='start_page[name]'][value='Start']"
    assert_select "select[name='start_page[columns]'] option[value='3'][selected]"
    assert_select "input[type='submit'][value='Create Start Page']"
  end

  test "should show start page info and form when start page exists" do
    start_page = StartPage.create!(user: @user, name: "Test Page", columns: 3)
    group = start_page.start_page_groups.create!(name: "Test Group", column: 1, position: 0)
    group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)

    get settings_start_page_url
    assert_response :success

    assert_select "h2", "Start Page"
    assert_select "li", text: /Name: Test Page/
    assert_select "li", text: /Groups: 1/
    assert_select "li", text: /Total Links: 1/
    assert_select "a[href='#{start_path}']", "View Start Page"
    assert_select "a[href='#{edit_start_path}']", "Edit Start Page"

    assert_select "h3", "Configuration"
    assert_select "form[action='#{start_path}']"
    assert_select "input[name='start_page[name]'][value='Test Page']"
    assert_select "select[name='start_page[columns]'] option[value='3'][selected]"
  end

  test "should show secondary nav with Start Page active" do
    get settings_start_page_url

    assert_select "ul.secondary-nav" do
      assert_select "li a[href='#{settings_path}']", "Main"
      assert_select "li a.active[href='#{settings_start_page_path}']", "Start Page"
    end
  end
end
