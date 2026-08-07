require "test_helper"

class TinylinksClientTest < ActiveSupport::TestCase
  setup do
    @connection = TinylinksConnection.create!(
      base_url: "https://links.example.com",
      token: "a-token",
      scopes: "search,visit"
    )
    @client = TinylinksClient.new(@connection)
  end

  # Stubbed at Net::HTTP.start, which is where the socket would be opened, so no
  # test touches the network. The block still runs, so the request we build is
  # exercised too.
  def stub_response(code:, body:)
    response = mock
    response.stubs(:code).returns(code.to_s)
    response.stubs(:body).returns(body)

    http = mock
    http.stubs(:request).returns(response)
    Net::HTTP.stubs(:start).yields(http).returns(response)
  end

  def stub_raising(error)
    Net::HTTP.stubs(:start).raises(error)
  end

  def expect_no_call
    Net::HTTP.expects(:start).never
  end

  ENVELOPE = {
    links: [
      { id: 1, url: "https://a.example", title: "Alpha", description: "long", tags: [ "x" ], visit_count: 3 },
      { id: 2, url: "https://b.example", title: "Beta", description: "long", tags: [], visit_count: 0 }
    ],
    meta: { page: 1, per_page: 12, total_items: 2, total_pages: 1 }
  }.to_json

  # --- search ---

  test "search returns only id, title and url" do
    stub_response(code: 200, body: ENVELOPE)

    results = @client.search("alpha")

    assert_equal 2, results.length
    assert_equal({ id: 1, title: "Alpha", url: "https://a.example" }, results.first)
  end

  test "search caps results at ten" do
    many = { links: (1..25).map { |i| { id: i, url: "https://#{i}.example", title: "T#{i}" } } }
    stub_response(code: 200, body: many.to_json)

    assert_equal 10, @client.search("t").length
  end

  test "search returns nothing for a blank query without calling out" do
    expect_no_call

    assert_equal [], @client.search("")
    assert_equal [], @client.search(nil)
  end

  test "search clears a previous failure once a call succeeds" do
    @connection.record_failure!("unauthorized")
    stub_response(code: 200, body: ENVELOPE)

    @client.search("alpha")

    assert_not @connection.reload.needs_reconnect?
  end

  # --- failure paths. Every one degrades to an empty list. ---

  test "search records a reconnect when the token is rejected" do
    stub_response(code: 401, body: '{"error":"unauthorized"}')

    assert_equal [], @client.search("alpha")
    assert @connection.reload.needs_reconnect?
    assert_match(/token/i, @connection.last_error)
  end

  test "search records a reconnect when the token lacks the scope" do
    stub_response(code: 403, body: '{"error":"insufficient_scope"}')

    assert_equal [], @client.search("alpha")
    assert @connection.reload.needs_reconnect?
  end

  test "search survives a server error without asking for a reconnect" do
    stub_response(code: 500, body: "boom")

    assert_equal [], @client.search("alpha")
    assert_not @connection.reload.needs_reconnect?
  end

  test "search survives malformed json" do
    stub_response(code: 200, body: "<html>not json</html>")

    assert_equal [], @client.search("alpha")
  end

  test "search survives a timeout" do
    stub_raising(Net::OpenTimeout)

    assert_equal [], @client.search("alpha")
  end

  test "search survives a read timeout" do
    stub_raising(Net::ReadTimeout)

    assert_equal [], @client.search("alpha")
  end

  test "search survives tinylinks being unreachable" do
    stub_raising(Errno::ECONNREFUSED)

    assert_equal [], @client.search("alpha")
  end

  test "search survives a socket error" do
    stub_raising(SocketError)

    assert_equal [], @client.search("alpha")
  end

  # --- record_visit ---

  test "record_visit reports success" do
    stub_response(code: 204, body: "")

    assert @client.record_visit(7)
  end

  test "record_visit reports failure without raising" do
    stub_raising(Errno::ECONNREFUSED)

    assert_not @client.record_visit(7)
  end

  test "record_visit does nothing without an id" do
    expect_no_call

    assert_not @client.record_visit(nil)
  end

  # --- no connection at all ---

  test "search returns nothing when the app was never connected" do
    expect_no_call

    assert_equal [], TinylinksClient.new(nil).search("alpha")
  end
end
