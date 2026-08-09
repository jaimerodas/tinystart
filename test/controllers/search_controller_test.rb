require "test_helper"

class SearchControllerTest < ActionDispatch::IntegrationTest
  setup do
    @user = users(:one)
    post session_url, params: { email: @user.email, password: "password123" }
  end

  test "requires authentication" do
    delete session_url
    get search_url(format: :json), params: { q: "alpha" }
    assert_redirected_to new_session_url
  end

  test "returns the client's results as a bare array" do
    results = [ { id: 1, title: "Alpha", url: "https://a.example" } ]
    ConnectionClient.any_instance.stubs(:search).returns(results)

    get search_url(format: :json), params: { q: "alpha" }

    assert_response :success
    json = JSON.parse(response.body)
    assert_equal 1, json.length
    assert_equal "Alpha", json.first["title"]
  end

  # The command bar treats an empty list as "no federated results" and keeps
  # showing local tiles, so every downstream failure has to land here as [].
  test "returns an empty array when the connected app is unreachable" do
    ConnectionClient.any_instance.stubs(:search).returns([])

    get search_url(format: :json), params: { q: "alpha" }

    assert_response :success
    assert_equal [], JSON.parse(response.body)
  end

  test "returns an empty array when the app was never connected" do
    Connection.delete_all

    get search_url(format: :json), params: { q: "alpha" }

    assert_response :success
    assert_equal [], JSON.parse(response.body)
  end

  # A token grants access to exactly one account on the other app. Before connections
  # were scoped to a user, any authenticated user's search reached whichever
  # connection happened to exist — leaking one archive into another's command bar.
  test "does not use another user's connection" do
    users(:two).create_connection!(
      base_url: "https://links.example.com", token: "user-twos-token"
    )
    # @user (users(:one)) has no connection of their own, so the client must be
    # handed nil — not the row that happens to exist.
    ConnectionClient.expects(:new).with(nil).returns(stub(search: []))

    get search_url(format: :json), params: { q: "anything" }

    assert_response :success
    assert_equal [], JSON.parse(response.body)
  end

  test "uses your own connection when you have one" do
    @user.create_connection!(base_url: "https://links.example.com", token: "mine")
    ConnectionClient.any_instance.stubs(:search).returns([ { id: 1, title: "Mine", url: "https://m.example" } ])

    get search_url(format: :json), params: { q: "anything" }

    assert_equal "Mine", JSON.parse(response.body).first["title"]
  end
end
