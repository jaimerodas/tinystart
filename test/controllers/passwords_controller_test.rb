require "test_helper"

class PasswordsControllerTest < ActionDispatch::IntegrationTest
  setup do
    @user = users(:one)
    ActionMailer::Base.deliveries.clear
  end

  test "GET new" do
    get new_password_url
    assert_response :success
  end

  test "POST create with existing email" do
    assert_difference "ActionMailer::Base.deliveries.size", 1 do
      post passwords_url, params: { email: @user.email }
    end
    assert_redirected_to new_session_url
    assert_equal "Password reset instructions sent (if user with that email address exists).", flash[:notice]
    mail = ActionMailer::Base.deliveries.last
    assert_equal [ @user.email ], mail.to
    assert_equal "Reset your password", mail.subject
  end

  test "POST create with non-existing email" do
    assert_no_difference "ActionMailer::Base.deliveries.size" do
      post passwords_url, params: { email: "nonexistent@example.com" }
    end
    assert_redirected_to new_session_url
    assert_equal "Password reset instructions sent (if user with that email address exists).", flash[:notice]
  end

  test "GET edit with valid token" do
    token = @user.password_reset_token
    get edit_password_url(token)
    assert_response :success
  end

  test "GET edit with invalid token" do
    get edit_password_url("invalid-token")
    assert_redirected_to new_password_url
    assert_equal "Password reset link is invalid or has expired.", flash[:alert]
  end

  test "PATCH update with valid password" do
    token = @user.password_reset_token
    new_password = "newpassword123"

    patch password_url(token), params: {
      password: new_password,
      password_confirmation: new_password
    }

    assert_redirected_to new_session_url
    assert_equal "Password has been reset.", flash[:notice]

    # Verify password was actually changed
    @user.reload
    assert @user.authenticate(new_password)
  end

  test "PATCH update with mismatched passwords" do
    token = @user.password_reset_token

    patch password_url(token), params: {
      password: "newpassword123",
      password_confirmation: "differentpassword"
    }

    assert_redirected_to edit_password_url(token)
    assert_equal "Passwords did not match.", flash[:alert]

    # Verify password was not changed
    @user.reload
    assert @user.authenticate("password123") # original password should still work
  end

  test "PATCH update with invalid token" do
    patch password_url("invalid-token"), params: {
      password: "newpassword123",
      password_confirmation: "newpassword123"
    }

    assert_redirected_to new_password_url
    assert_equal "Password reset link is invalid or has expired.", flash[:alert]
  end


  test "password reset flow integration" do
    # Step 1: Request password reset
    post passwords_url, params: { email: @user.email }
    assert_redirected_to new_session_url
    assert_equal "Password reset instructions sent (if user with that email address exists).", flash[:notice]

    # Step 2: Verify email was sent
    mail = ActionMailer::Base.deliveries.last
    assert_not_nil mail, "Should have sent an email"
    assert_equal [ @user.email ], mail.to
    assert_equal "Reset your password", mail.subject

    # Step 3: Use the password reset token directly (since email parsing is complex in tests)
    token = @user.password_reset_token

    # Step 4: Visit reset link
    get edit_password_url(token)
    assert_response :success

    # Step 5: Reset password
    new_password = "mynewpassword123"
    patch password_url(token), params: {
      password: new_password,
      password_confirmation: new_password
    }

    assert_redirected_to new_session_url
    assert_equal "Password has been reset.", flash[:notice]

    # Step 6: Verify can login with new password
    @user.reload
    assert @user.authenticate(new_password)
    assert_not @user.authenticate("password123") # old password should not work
  end
end
