require "test_helper"

class ConnectionTest < ActiveSupport::TestCase
  setup do
    @connection = Connection.create!(user: users(:one), base_url: "https://links.example.com", token: "t")
  end

  test "belongs to the user who approved it" do
    assert_equal users(:one), @connection.user
    assert_equal @connection, users(:one).connection
  end

  test "a user can only have one connection" do
    duplicate = Connection.new(user: users(:one), base_url: "https://other.example.com", token: "u")
    assert_not duplicate.valid?
    assert_includes duplicate.errors[:user_id], "has already been taken"
  end

  test "different users can each have their own" do
    other = Connection.new(user: users(:two), base_url: "https://other.example.com", token: "u")
    assert other.valid?
  end

  test "requires a base_url and a token" do
    connection = Connection.new(user: users(:two))
    assert_not connection.valid?
    assert_includes connection.errors[:base_url], "can't be blank"
    assert_includes connection.errors[:token], "can't be blank"
  end

  # The command bar names its federated section after the host it came from.
  test "hostname drops the scheme from the base_url" do
    assert_equal "links.example.com", @connection.hostname
  end

  test "hostname drops a port too" do
    @connection.update!(base_url: "http://localhost:3500")

    assert_equal "localhost", @connection.hostname
  end

  # Stored the way the other app sends them; read by a person.
  test "scope_list writes the commas out as prose" do
    @connection.update!(scopes: "search,visit")

    assert_equal "search, visit", @connection.scope_list
  end

  test "scope_list tidies whatever spacing it was given" do
    @connection.update!(scopes: " search ,, visit ")

    assert_equal "search, visit", @connection.scope_list
  end

  # Nil rather than "", so the page can say "full access" instead of nothing.
  test "scope_list is nil when there are no scopes" do
    @connection.update!(scopes: nil)
    assert_nil @connection.scope_list

    @connection.update!(scopes: "")
    assert_nil @connection.scope_list
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
    assert_nil Connection.find_by(id: @connection.id)
  end
end
