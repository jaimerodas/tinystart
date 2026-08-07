require "test_helper"

class SessionTest < ActiveSupport::TestCase
  def setup
    @user = users(:one)
  end

  test "should belong to user" do
    session = Session.new(user: @user, ip_address: "127.0.0.1", user_agent: "Test Agent")
    assert session.user == @user
  end

  test "should have session lifetime constant" do
    assert_equal 30.days, Session::SESSION_LIFETIME
  end

  test "expired? should return true for old sessions" do
    session = sessions(:expired)
    assert session.expired?
  end

  test "expired? should return false for recent sessions" do
    session = sessions(:recent)
    assert_not session.expired?
  end

  test "expired scope should return expired sessions" do
    expired_sessions = Session.expired
    assert_includes expired_sessions, sessions(:expired)
    assert_not_includes expired_sessions, sessions(:recent)
  end

  test "active scope should return non-expired sessions" do
    active_sessions = Session.active
    assert_includes active_sessions, sessions(:recent)
    assert_not_includes active_sessions, sessions(:expired)
  end

  test "should require user" do
    session = Session.new(ip_address: "127.0.0.1", user_agent: "Test Agent")
    assert_not session.valid?
    assert_includes session.errors[:user], "must exist"
  end

  test "should create session with user agent and ip address" do
    session = Session.create!(
      user: @user,
      ip_address: "192.168.1.1",
      user_agent: "Mozilla/5.0 Test Browser"
    )

    assert session.persisted?
    assert_equal "192.168.1.1", session.ip_address
    assert_equal "Mozilla/5.0 Test Browser", session.user_agent
    assert_equal @user, session.user
  end

  test "should allow multiple sessions per user" do
    Session.destroy_all  # Clear existing sessions for clean test
    session1 = Session.create!(user: @user, ip_address: "127.0.0.1", user_agent: "Browser 1")
    session2 = Session.create!(user: @user, ip_address: "127.0.0.2", user_agent: "Browser 2")

    assert session1.persisted?
    assert session2.persisted?
    assert_equal 2, @user.sessions.count
  end

  test "should destroy sessions when user is destroyed" do
    session = Session.create!(user: @user, ip_address: "127.0.0.1", user_agent: "Test")
    session_id = session.id

    @user.destroy

    assert_raises(ActiveRecord::RecordNotFound) do
      Session.find(session_id)
    end
  end

  test "should set expires_at on creation" do
    session = Session.create!(
      user: @user,
      ip_address: "127.0.0.1",
      user_agent: "Test Agent"
    )

    assert_not_nil session.expires_at
    assert_in_delta Time.current + 30.days, session.expires_at, 1.second
  end

  test "extend_session! should update expires_at" do
    session = Session.create!(
      user: @user,
      ip_address: "127.0.0.1",
      user_agent: "Test Agent"
    )

    original_expires_at = session.expires_at

    # Travel forward in time
    travel 5.days do
      session.extend_session!
      session.reload

      assert session.expires_at > original_expires_at
      assert_in_delta Time.current + 30.days, session.expires_at, 1.second
    end
  end

  test "extend_session! should not change created_at" do
    session = Session.create!(
      user: @user,
      ip_address: "127.0.0.1",
      user_agent: "Test Agent"
    )

    original_created_at = session.created_at

    travel 5.days do
      session.extend_session!
      session.reload

      assert_equal original_created_at, session.created_at
    end
  end
end
