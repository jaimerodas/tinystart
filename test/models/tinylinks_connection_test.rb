require "test_helper"

class TinylinksConnectionTest < ActiveSupport::TestCase
  setup do
    @connection = TinylinksConnection.create!(base_url: "https://links.example.com", token: "t")
  end

  test "current returns the most recently created row" do
    newer = TinylinksConnection.create!(base_url: "https://other.example.com", token: "u")
    assert_equal newer, TinylinksConnection.current
  end

  test "requires a base_url and a token" do
    connection = TinylinksConnection.new
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

  test "needs_reconnect? is true at class level when never connected" do
    TinylinksConnection.delete_all
    assert TinylinksConnection.needs_reconnect?
  end
end
