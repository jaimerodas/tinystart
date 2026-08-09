require "net/http"

# Drives a connected app's OAuth 2.0 Device Authorization Grant (RFC 8628) so
# this app can get its own scoped token.
#
# Deliberately non-blocking: `start` opens a grant, `check` reports where it got
# to. The waiting happens in the browser, not here.
class DeviceFlow
  SCOPES = "search,visit".freeze
  CLIENT_NAME = "tinystart".freeze
  OPEN_TIMEOUT = 2
  READ_TIMEOUT = 4

  Grant = Struct.new(:device_code, :verification_url, :expires_in, :interval, keyword_init: true)

  def initialize(base_url)
    @base_url = base_url
  end

  # => Grant, or nil if the other app couldn't be reached or refused.
  def start
    body = post("/api/v1/device_authorizations", client_name: CLIENT_NAME, scopes: SCOPES)
    return nil if body.nil? || body["error"]

    Grant.new(
      device_code: body["device_code"],
      verification_url: body["verification_url"],
      expires_in: body["expires_in"],
      interval: body["interval"]
    )
  end

  # => [status, token_body]
  #    status is one of :approved, :pending, :denied, :expired, :unreachable
  def check(device_code)
    body = post("/api/v1/device_authorizations/token", device_code: device_code)
    return [ :unreachable, nil ] if body.nil?

    case body["error"]
    when nil then [ :approved, body ]
    when "authorization_pending" then [ :pending, nil ]
    when "access_denied" then [ :denied, nil ]
    when "expired_token" then [ :expired, nil ]
    else [ :expired, nil ]
    end
  end

  private

  def post(path, params)
    uri = URI.join(@base_url, path)

    response = Net::HTTP.start(uri.host, uri.port,
                               use_ssl: uri.scheme == "https",
                               open_timeout: OPEN_TIMEOUT,
                               read_timeout: READ_TIMEOUT) do |http|
      request = Net::HTTP::Post.new(uri)
      request.set_form_data(params)
      request["Accept"] = "application/json"
      http.request(request)
    end

    JSON.parse(response.body)
  rescue JSON::ParserError
    Rails.logger.warn("[DeviceFlow] #{@base_url} returned something that isn't JSON")
    nil
  rescue Net::OpenTimeout, Net::ReadTimeout, SocketError, SystemCallError, OpenSSL::SSL::SSLError => e
    Rails.logger.warn("[DeviceFlow] could not reach #{@base_url}: #{e.class}")
    nil
  end
end
