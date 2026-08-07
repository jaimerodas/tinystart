require "test_helper"

class Settings::TinylinksConnectionsControllerTest < ActionDispatch::IntegrationTest
  setup do
    @admin = users(:admin)
  end

  def login_as(user)
    post session_url, params: { email: user.email, password: "password123" }
  end

  def grant(device_code: "abc")
    TinylinksDeviceFlow::Grant.new(
      device_code: device_code,
      verification_url: "https://links.example.com/device/new?code=#{device_code}",
      expires_in: 600,
      interval: 5
    )
  end

  # --- access ---

  test "requires authentication" do
    get settings_tinylinks_url
    assert_redirected_to new_session_url
  end

  test "requires an admin" do
    login_as users(:one)
    get settings_tinylinks_url
    assert_redirected_to root_path
  end

  test "shows the connect form when never connected" do
    login_as @admin
    get settings_tinylinks_url

    assert_response :success
    assert_select "form.connect-form"
  end

  test "shows the connection when healthy" do
    login_as @admin
    TinylinksConnection.create!(base_url: "https://links.example.com", token: "t", scopes: "search,visit")

    get settings_tinylinks_url

    assert_select ".connection-status.connected"
    assert_select "form.connect-form", false
  end

  test "shows the error and the form again when the token was rejected" do
    login_as @admin
    TinylinksConnection.create!(base_url: "https://links.example.com", token: "t")
      .record_failure!("tinylinks rejected the token")

    get settings_tinylinks_url

    assert_select ".connection-status.disconnected"
    assert_select "form.connect-form"
  end

  # --- starting a flow ---

  test "create opens a grant and shows the waiting state" do
    login_as @admin
    TinylinksDeviceFlow.any_instance.stubs(:start).returns(grant)

    post settings_tinylinks_url, params: { base_url: "https://links.example.com" }
    follow_redirect!

    assert_select ".connection-status.pending"
    assert_select "a[href=?]", "https://links.example.com/device/new?code=abc"
  end

  test "create says so when tinylinks cannot be reached" do
    login_as @admin
    TinylinksDeviceFlow.any_instance.stubs(:start).returns(nil)

    post settings_tinylinks_url, params: { base_url: "https://links.example.com" }

    assert_redirected_to settings_tinylinks_path
    assert_match(/[Cc]ould not reach/, flash[:alert])
  end

  # --- polling ---

  test "poll reports idle with no grant in flight" do
    login_as @admin
    get poll_settings_tinylinks_url
    assert_equal "idle", JSON.parse(response.body)["status"]
  end

  test "poll reports pending while waiting" do
    login_as @admin
    TinylinksDeviceFlow.any_instance.stubs(:start).returns(grant)
    post settings_tinylinks_url

    TinylinksDeviceFlow.any_instance.stubs(:check).returns([ :pending, nil ])
    get poll_settings_tinylinks_url

    assert_equal "pending", JSON.parse(response.body)["status"]
  end

  # A blip mid-flow must not look like a denial — the grant is still good.
  test "poll keeps waiting when tinylinks is briefly unreachable" do
    login_as @admin
    TinylinksDeviceFlow.any_instance.stubs(:start).returns(grant)
    post settings_tinylinks_url

    TinylinksDeviceFlow.any_instance.stubs(:check).returns([ :unreachable, nil ])
    get poll_settings_tinylinks_url

    assert_equal "pending", JSON.parse(response.body)["status"]
  end

  test "poll stores the connection once approved" do
    login_as @admin
    TinylinksDeviceFlow.any_instance.stubs(:start).returns(grant)
    post settings_tinylinks_url, params: { base_url: "https://links.example.com" }

    TinylinksDeviceFlow.any_instance.stubs(:check).returns([
      :approved, { "token" => "a-token", "scopes" => [ "search", "visit" ],
                   "expires_at" => "2026-11-05T00:00:00Z" }
    ])

    assert_difference "TinylinksConnection.count", 1 do
      get poll_settings_tinylinks_url
    end

    assert_equal "connected", JSON.parse(response.body)["status"]

    connection = TinylinksConnection.current
    assert_equal "https://links.example.com", connection.base_url
    assert_equal "a-token", connection.token
    assert_equal "search,visit", connection.scopes
  end

  test "poll replaces an old connection rather than piling up rows" do
    login_as @admin
    TinylinksConnection.create!(base_url: "https://old.example.com", token: "old")
    TinylinksDeviceFlow.any_instance.stubs(:start).returns(grant)
    post settings_tinylinks_url, params: { base_url: "https://links.example.com" }
    TinylinksDeviceFlow.any_instance.stubs(:check).returns([ :approved, { "token" => "new" } ])

    get poll_settings_tinylinks_url

    assert_equal 1, TinylinksConnection.count
    assert_equal "new", TinylinksConnection.current.token
  end

  test "poll reports a denial and forgets the grant" do
    login_as @admin
    TinylinksDeviceFlow.any_instance.stubs(:start).returns(grant)
    post settings_tinylinks_url

    TinylinksDeviceFlow.any_instance.stubs(:check).returns([ :denied, nil ])
    get poll_settings_tinylinks_url

    assert_equal "denied", JSON.parse(response.body)["status"]

    # The grant is gone, so the next poll has nothing to wait on.
    get poll_settings_tinylinks_url
    assert_equal "idle", JSON.parse(response.body)["status"]
  end

  # --- disconnecting ---

  test "destroy removes the connection" do
    login_as @admin
    TinylinksConnection.create!(base_url: "https://links.example.com", token: "t")

    assert_difference "TinylinksConnection.count", -1 do
      delete settings_tinylinks_url
    end

    assert_redirected_to settings_tinylinks_path
  end

  test "destroy requires an admin" do
    login_as users(:one)
    TinylinksConnection.create!(base_url: "https://links.example.com", token: "t")

    assert_no_difference "TinylinksConnection.count" do
      delete settings_tinylinks_url
    end
  end
end
