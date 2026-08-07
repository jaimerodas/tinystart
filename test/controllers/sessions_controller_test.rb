require "test_helper"

class SessionsControllerTest < ActionDispatch::IntegrationTest
  setup do
    @user = users(:one)
  end

  test "GET new" do
    get new_session_url
    assert_response :success
  end

  test "login form opts out of turbo so theme attributes render after login" do
    get new_session_url
    assert_select "form[action=?][data-turbo=false]", session_path
  end

  test "logout button opts out of turbo so theme attributes reset after logout" do
    post session_url, params: { email: @user.email, password: "password123" }
    # root is the start page, which runs a chrome-free layout; the header with
    # the logout button lives on the application layout.
    get settings_url
    assert_select "header form[action=?][data-turbo=false]", session_path
  end

  test "login with valid credentials" do
    Session.destroy_all  # Clear existing sessions for clean test
    assert_difference "Session.count", 1 do
      post session_url, params: { email: @user.email, password: "password123" }
    end
    assert_not_nil cookies["session_id"]
    assert_redirected_to root_url
  end

  # Signups wait for an admin. The gate lives in SessionsController#create, so
  # correct credentials alone are not enough.

  test "unapproved user cannot log in even with the right password" do
    pending = User.create!(email: "pending@example.com", password: "password123")
    assert_not pending.approved?

    assert_no_difference "Session.count" do
      post session_url, params: { email: pending.email, password: "password123" }
    end

    assert_redirected_to new_session_url
    assert_equal "Try another email address or password.", flash[:alert]
  end

  test "user can log in once an admin approves them" do
    pending = User.create!(email: "pending@example.com", password: "password123")
    pending.update!(approved: true)

    assert_difference "Session.count", 1 do
      post session_url, params: { email: pending.email, password: "password123" }
    end

    assert_redirected_to root_url
  end

  test "login with invalid credentials" do
    assert_no_difference "Session.count" do
      post session_url, params: { email: @user.email, password: "wrongpassword" }
    end
    assert_redirected_to new_session_url
    assert_equal "Try another email address or password.", flash[:alert]
  end

  test "access protected page redirects to login" do
    get settings_url
    assert_redirected_to new_session_url
  end

  test "logout" do
    Session.destroy_all  # Clear existing sessions for clean test
    # login first
    post session_url, params: { email: @user.email, password: "password123" }
    assert_equal 1, Session.count

    delete session_url
    assert_equal 0, Session.count
    # cookie may be cleared as empty string in test environment
    assert cookies["session_id"].blank?
    assert_redirected_to new_session_url
  end

  test "login redirects to intended page after authentication" do
    get settings_url
    assert_redirected_to new_session_url
    post session_url, params: { email: @user.email, password: "password123" }
    assert_redirected_to settings_url
  end

  test "login cleans up expired sessions" do
    # Clear existing sessions to start fresh
    Session.destroy_all

    # Create expired sessions for the user
    expired_session1 = Session.create!(
      user: @user,
      ip_address: "192.168.1.1",
      user_agent: "Old Browser 1",
      created_at: 35.days.ago,
      expires_at: 5.days.ago
    )
    expired_session2 = Session.create!(
      user: @user,
      ip_address: "192.168.1.2",
      user_agent: "Old Browser 2",
      created_at: 40.days.ago,
      expires_at: 10.days.ago
    )

    # Create a recent session that should not be cleaned up
    recent_session = Session.create!(
      user: @user,
      ip_address: "192.168.1.3",
      user_agent: "Recent Browser",
      created_at: 5.days.ago,
      expires_at: 25.days.from_now
    )

    assert_equal 3, @user.sessions.count
    assert_equal 2, @user.sessions.expired.count

    # Login should clean up expired sessions
    post session_url, params: { email: @user.email, password: "password123" }

    @user.reload
    assert_equal 2, @user.sessions.count # recent + new login session
    assert_equal 0, @user.sessions.expired.count

    # Verify the expired sessions were actually deleted
    assert_raises(ActiveRecord::RecordNotFound) { expired_session1.reload }
    assert_raises(ActiveRecord::RecordNotFound) { expired_session2.reload }

    # Verify recent session still exists
    assert_nothing_raised { recent_session.reload }
  end

  test "expired session cookie requires re-authentication" do
    expired_session = sessions(:expired)

    # Simulate having an expired session by setting request environment
    get settings_url, headers: { "HTTP_COOKIE" => "session_id=#{expired_session.id}" }

    # Should redirect to login despite having a session cookie
    assert_redirected_to new_session_url
  end

  test "multiple sessions allowed per user" do
    Session.destroy_all

    # First login
    post session_url, params: { email: @user.email, password: "password123" }

    # Second login (simulating different browser/device)
    post session_url, params: { email: @user.email, password: "password123" }

    assert_equal 2, @user.sessions.active.count
  end
end
