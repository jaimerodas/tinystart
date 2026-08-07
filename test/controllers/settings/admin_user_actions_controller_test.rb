require "test_helper"

class Settings::AdminUserActionsControllerTest < ActionDispatch::IntegrationTest
  setup do
    @admin = users(:admin)
    @pending = User.create!(email: "pending@example.com", password: "password123")
  end

  def login_as(user)
    post session_url, params: { email: user.email, password: "password123" }
  end

  test "approve requires an admin" do
    login_as users(:one)

    assert_no_changes -> { @pending.reload.approved? } do
      post settings_user_approve_url(@pending)
    end

    assert_redirected_to root_path
  end

  test "approve flips a pending user to approved" do
    login_as @admin

    post settings_user_approve_url(@pending)

    assert @pending.reload.approved?
    assert_redirected_to settings_users_path
  end

  test "approve toggles an approved user back to blocked" do
    login_as @admin
    @pending.update!(approved: true)

    post settings_user_approve_url(@pending)

    assert_not @pending.reload.approved?
  end

  test "password_reset mails the user" do
    login_as @admin

    assert_difference "ActionMailer::Base.deliveries.size", 1 do
      post settings_user_password_reset_url(@pending)
    end

    assert_equal [ @pending.email ], ActionMailer::Base.deliveries.last.to
    assert_redirected_to settings_users_path
  end

  test "password_reset requires an admin" do
    login_as users(:one)

    assert_no_difference "ActionMailer::Base.deliveries.size" do
      post settings_user_password_reset_url(@pending)
    end

    assert_redirected_to root_path
  end
end
