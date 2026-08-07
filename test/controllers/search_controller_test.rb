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
    TinylinksClient.any_instance.stubs(:search).returns(results)

    get search_url(format: :json), params: { q: "alpha" }

    assert_response :success
    json = JSON.parse(response.body)
    assert_equal 1, json.length
    assert_equal "Alpha", json.first["title"]
  end

  # The command bar treats an empty list as "no federated results" and keeps
  # showing local tiles, so every downstream failure has to land here as [].
  test "returns an empty array when tinylinks is unreachable" do
    TinylinksClient.any_instance.stubs(:search).returns([])

    get search_url(format: :json), params: { q: "alpha" }

    assert_response :success
    assert_equal [], JSON.parse(response.body)
  end

  test "returns an empty array when the app was never connected" do
    TinylinksConnection.delete_all

    get search_url(format: :json), params: { q: "alpha" }

    assert_response :success
    assert_equal [], JSON.parse(response.body)
  end
end
