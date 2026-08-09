require "test_helper"

class VisitsControllerTest < ActionDispatch::IntegrationTest
  setup do
    @user = users(:one)
    post session_url, params: { email: @user.email, password: "password123" }
  end

  test "requires authentication" do
    delete session_url
    post visits_url, params: { link_id: 7 }
    assert_redirected_to new_session_url
  end

  test "forwards the visit to the connected app and answers 204" do
    ConnectionClient.any_instance.expects(:record_visit).with("7").returns(true)

    post visits_url, params: { link_id: 7 }

    assert_response :no_content
  end

  # Tracking is fire-and-forget: a failure upstream must not surface as an error
  # on a click the user already made.
  test "still answers 204 when the connected app cannot be reached" do
    ConnectionClient.any_instance.stubs(:record_visit).returns(false)

    post visits_url, params: { link_id: 7 }

    assert_response :no_content
  end
end
