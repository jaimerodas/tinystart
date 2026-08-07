require "test_helper"

class UsersControllerTest < ActionDispatch::IntegrationTest
  def admin_login
    post session_url, params: { email: users(:admin)[:email], password: "password123" }
  end

  def login
    post session_url, params: { email: @user[:email], password: "password123" }
  end

  setup do
    @user = users(:one)
  end

  test "should get index" do
    admin_login
    get settings_users_url
    assert_response :success
  end

  test "should get new" do
    get sign_up_url
    assert_response :success
  end

  test "should create user" do
    assert_difference("User.count") do
      post sign_up_url, params: { user: { email: "useremail@email.com", password: "password" } }
    end

    assert_redirected_to root_url
  end

  test "should not create user with a duplicate email" do
    assert_no_difference("User.count") do
      post sign_up_url, params: { user: { email: @user[:email], password: "password" } }
    end

    assert_response :unprocessable_content
  end

  test "should redirect signed in user away from new" do
    login

    get sign_up_url

    assert_redirected_to root_path
  end

  test "should redirect signed in user away from create without creating a user" do
    login

    assert_no_difference("User.count") do
      post sign_up_url, params: { user: { email: "another@email.com", password: "password" } }
    end

    assert_redirected_to root_path
  end
end
