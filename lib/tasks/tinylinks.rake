require "net/http"

namespace :tinylinks do
  desc "Connect this app to tinylinks via its device flow (BASE_URL=https://links.pati.to)"
  task connect: :environment do
    base_url = ENV.fetch("BASE_URL", "https://links.pati.to")

    grant = TinylinksDeviceFlow.new(base_url)

    puts "Asking #{base_url} for authorization..."
    request = grant.request!
    puts
    puts "Open this in a browser where you're signed in to tinylinks, then approve:"
    puts
    puts "    #{request['verification_url']}"
    puts
    puts "Waiting for approval (expires in #{request['expires_in'] / 60} minutes)..."

    token = grant.poll!(interval: request["interval"], expires_in: request["expires_in"])

    if token.nil?
      abort "Gave up waiting. Nothing was saved; run the task again."
    end

    TinylinksConnection.delete_all
    TinylinksConnection.create!(
      base_url: base_url,
      token: token["token"],
      scopes: Array(token["scopes"]).join(","),
      token_expires_at: token["expires_at"]
    )

    puts "Connected. Scopes: #{Array(token['scopes']).join(', ')}"
  end

  desc "Show whether this app can reach tinylinks"
  task status: :environment do
    connection = TinylinksConnection.current

    if connection.nil?
      puts "Not connected. Run: bin/rails tinylinks:connect"
      next
    end

    puts "Base URL: #{connection.base_url}"
    puts "Scopes:   #{connection.scopes}"
    puts "Expires:  #{connection.token_expires_at || 'unknown'}"

    if connection.needs_reconnect?
      puts "Status:   NEEDS RECONNECT — #{connection.last_error}"
    else
      results = TinylinksClient.new(connection).search("a")
      puts "Status:   OK (a test search returned #{results.length} result(s))"
    end
  end
end

# Small helper so the rake task stays readable. Only used at setup time.
class TinylinksDeviceFlow
  def initialize(base_url)
    @base_url = base_url
  end

  def request!
    post("/api/v1/device_authorizations",
         client_name: "tinystart", scopes: "search,visit").tap do |body|
      abort "tinylinks refused the request: #{body['error']}" if body["error"]
      @device_code = body["device_code"]
    end
  end

  # Polls until approved, denied, or the grant expires.
  def poll!(interval:, expires_in:)
    deadline = Time.current + expires_in

    while Time.current < deadline
      sleep interval
      body = post("/api/v1/device_authorizations/token", device_code: @device_code)

      case body["error"]
      when nil            then return body
      when "authorization_pending" then next
      when "access_denied" then abort "Approval was denied."
      when "expired_token" then return nil
      else abort "tinylinks said: #{body['error']}"
      end
    end

    nil
  end

  private

  def post(path, params)
    uri = URI.join(@base_url, path)
    response = Net::HTTP.post_form(uri, params)
    JSON.parse(response.body)
  rescue JSON::ParserError
    abort "tinylinks returned something that isn't JSON (#{response&.code})."
  end
end
