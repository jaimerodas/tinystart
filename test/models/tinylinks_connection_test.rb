require "test_helper"

class TinylinksConnectionTest < ActiveSupport::TestCase
  setup do
    @connection = TinylinksConnection.create!(user: users(:one), base_url: "https://links.example.com", token: "t")
  end

  test "belongs to the user who approved it" do
    assert_equal users(:one), @connection.user
    assert_equal @connection, users(:one).tinylinks_connection
  end

  test "a user can only have one connection" do
    duplicate = TinylinksConnection.new(user: users(:one), base_url: "https://other.example.com", token: "u")
    assert_not duplicate.valid?
    assert_includes duplicate.errors[:user_id], "has already been taken"
  end

  test "different users can each have their own" do
    other = TinylinksConnection.new(user: users(:two), base_url: "https://other.example.com", token: "u")
    assert other.valid?
  end

  test "requires a base_url and a token" do
    connection = TinylinksConnection.new(user: users(:two))
    assert_not connection.valid?
    assert_includes connection.errors[:base_url], "can't be blank"
    assert_includes connection.errors[:token], "can't be blank"
  end

  test "a fresh connection does not need reconnecting" do
    assert_not @connection.needs_reconnect?
  end

  test "recording a failure marks it as needing a reconnect" do
    @connection.record_failure!("token rejected")

    assert @connection.reload.needs_reconnect?
    assert_equal "token rejected", @connection.last_error
    assert_not_nil @connection.last_failed_at
  end

  test "clearing a failure resets it" do
    @connection.record_failure!("token rejected")

    @connection.clear_failure!

    assert_not @connection.reload.needs_reconnect?
    assert_nil @connection.last_failed_at
  end

  test "is destroyed along with its user" do
    users(:one).destroy
    assert_nil TinylinksConnection.find_by(id: @connection.id)
  end
end
