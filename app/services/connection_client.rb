require "net/http"

# Talks to a connected app's API on behalf of the command bar.
#
# Everything here degrades to an empty result: federated search is a bonus on
# top of the local tiles, so a slow or absent app must never break the start
# page or hold up a keystroke. A rejected token is the one failure worth
# surfacing, so it's recorded on the connection for the UI to pick up.
class ConnectionClient
  MAX_RESULTS = 10
  OPEN_TIMEOUT = 2
  READ_TIMEOUT = 4

  def initialize(connection)
    @connection = connection
  end

  # => [{ id:, title:, url: }], newest-relevance first, capped at MAX_RESULTS.
  def search(query)
    return [] if @connection.nil? || query.blank?

    response = get("/api/v1/search", q: query, per_page: MAX_RESULTS)
    return [] unless response

    links = JSON.parse(response.body).fetch("links", [])
    @connection.clear_failure!

    links.first(MAX_RESULTS).map do |link|
      { id: link["id"], title: link["title"], url: link["url"] }
    end
  rescue JSON::ParserError => e
    log_failure("#{host} returned something that isn't JSON: #{e.message}")
    []
  end

  # Fire-and-forget: true when the other app acknowledged, false otherwise.
  def record_visit(link_id)
    return false if @connection.nil? || link_id.blank?

    !post("/api/v1/links/#{link_id}/visit").nil?
  end

  private

  def get(path, params)
    uri = build_uri(path)
    uri.query = URI.encode_www_form(params)
    perform(Net::HTTP::Get.new(uri), uri)
  end

  def post(path)
    uri = build_uri(path)
    perform(Net::HTTP::Post.new(uri), uri)
  end

  def build_uri(path)
    URI.join(@connection.base_url, path)
  end

  # Returns the response on success, nil on any failure.
  def perform(request, uri)
    request["Authorization"] = "Bearer #{@connection.token}"
    request["Accept"] = "application/json"

    response = Net::HTTP.start(uri.host, uri.port,
                               use_ssl: uri.scheme == "https",
                               open_timeout: OPEN_TIMEOUT,
                               read_timeout: READ_TIMEOUT) do |http|
      http.request(request)
    end

    handle(response)
  rescue Net::OpenTimeout, Net::ReadTimeout
    log_failure("#{host} timed out")
    nil
  rescue SocketError, SystemCallError, OpenSSL::SSL::SSLError => e
    log_failure("could not reach #{host}: #{e.class}")
    nil
  end

  def handle(response)
    case response.code.to_i
    when 200..299
      response
    when 401
      needs_reconnect("#{host} rejected the token — reconnect to restore search")
    when 403
      needs_reconnect("the #{host} token is missing a scope — reconnect to restore search")
    else
      # A bad gateway or a 500 is the other app's problem, not a credential
      # problem; asking the user to reconnect would be wrong.
      log_failure("#{host} answered #{response.code}")
      nil
    end
  end

  # What the messages call the other end. These are read by a person — on the
  # start page for a rejected token, in the log otherwise — so they name the
  # host rather than a product.
  def host
    @connection&.hostname || "the connected app"
  end

  def needs_reconnect(message)
    Rails.logger.warn("[ConnectionClient] #{message}")
    @connection.record_failure!(message)
    nil
  end

  def log_failure(message)
    Rails.logger.warn("[ConnectionClient] #{message}")
    nil
  end
end
