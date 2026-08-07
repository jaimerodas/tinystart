require "test_helper"

class Settings::PasswordsControllerTest < ActionDispatch::IntegrationTest
  def login
    post session_url, params: { email: users(:one)[:email], password: "password123" }
  end

  test "should get edit" do
    login
    get edit_settings_password_url
    assert_response :success
  end

  test "should get update" do
    login
    patch settings_password_url, params: { user: { existing_password: "password123", new_password: "testtesttest" } }
    assert_redirected_to settings_url
  end

  test "should fail when existing password is wrong" do
    login
    patch settings_password_url, params: { user: { existing_password: "wrong", new_password: "testtest" } }
    assert_response :unprocessable_content
  end

  test "should fail when new password is blank" do
    login
    patch settings_password_url, params: { user: { existing_password: "password123", new_password: "" } }
    assert_response :unprocessable_content
  end
end
