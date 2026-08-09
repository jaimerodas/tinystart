require "test_helper"

class DeviceFlowTest < ActiveSupport::TestCase
  setup do
    @flow = DeviceFlow.new("https://links.example.com")
  end

  def stub_response(body)
    response = mock
    response.stubs(:body).returns(body)
    http = mock
    http.stubs(:request).returns(response)
    Net::HTTP.stubs(:start).yields(http).returns(response)
  end

  def stub_raising(error)
    Net::HTTP.stubs(:start).raises(error)
  end

  # --- start ---

  test "start returns the grant details" do
    stub_response({
      device_code: "abc", verification_url: "https://links.example.com/device/new?code=abc",
      expires_in: 600, interval: 5
    }.to_json)

    grant = @flow.start

    assert_equal "abc", grant.device_code
    assert_equal "https://links.example.com/device/new?code=abc", grant.verification_url
    assert_equal 600, grant.expires_in
    assert_equal 5, grant.interval
  end

  test "start returns nothing when the app refuses" do
    stub_response({ error: "invalid_scope" }.to_json)

    assert_nil @flow.start
  end

  test "start returns nothing when the app is unreachable" do
    stub_raising(Errno::ECONNREFUSED)

    assert_nil @flow.start
  end

  test "start returns nothing when the response is not json" do
    stub_response("<html>nope</html>")

    assert_nil @flow.start
  end

  # --- check ---

  test "check reports approval and hands back the token" do
    stub_response({ token: "t", expires_at: "2026-11-05T00:00:00Z", scopes: [ "search", "visit" ] }.to_json)

    status, token = @flow.check("abc")

    assert_equal :approved, status
    assert_equal "t", token["token"]
  end

  test "check reports pending while nobody has approved it" do
    stub_response({ error: "authorization_pending" }.to_json)

    assert_equal :pending, @flow.check("abc").first
  end

  test "check reports denial" do
    stub_response({ error: "access_denied" }.to_json)

    assert_equal :denied, @flow.check("abc").first
  end

  test "check reports expiry" do
    stub_response({ error: "expired_token" }.to_json)

    assert_equal :expired, @flow.check("abc").first
  end

  # A blip mid-flow is not the same as a denial — the caller keeps waiting.
  test "check reports unreachable separately from denial" do
    stub_raising(Net::ReadTimeout)

    assert_equal :unreachable, @flow.check("abc").first
  end
end
