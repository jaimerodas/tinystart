class Settings::ConnectionsController < ApplicationController
  # No admin gate: you connect your own account on the other app, which needs
  # no privilege. The scoping below is what keeps one user's archive out of
  # another's command bar.
  DEFAULT_BASE_URL = "https://links.pati.to".freeze

  # GET /settings/connections
  def show
    @connection = current_user.connection
    @pending = pending_grant
  end

  # POST /settings/connections
  # Opens a grant on the other app and parks it in the session while the
  # browser polls. Short-lived (10 minutes) and single-use, so it needs no
  # table.
  def create
    grant = DeviceFlow.new(base_url).start

    if grant.nil?
      redirect_to settings_connections_path,
        alert: "Could not reach #{base_url}. Check the address and try again."
      return
    end

    session[:connection_grant] = {
      "device_code" => grant.device_code,
      "verification_url" => grant.verification_url,
      "base_url" => base_url,
      "expires_at" => grant.expires_in.seconds.from_now.iso8601
    }

    redirect_to settings_connections_path
  end

  # GET /settings/connections/poll
  # Answers the Stimulus poller. Never redirects — the page decides what to do.
  def poll
    grant = pending_grant
    return render json: { status: "idle" } if grant.nil?

    status, token = DeviceFlow.new(grant["base_url"]).check(grant["device_code"])

    case status
    when :approved
      store_connection(grant["base_url"], token)
      session.delete(:connection_grant)
      render json: { status: "connected" }
    when :pending, :unreachable
      # An unreachable app mid-flow is usually a blip; keep waiting until the
      # grant expires on its own.
      render json: { status: "pending" }
    else
      session.delete(:connection_grant)
      render json: { status: status.to_s }
    end
  end

  # DELETE /settings/connections
  def destroy
    current_user.connection&.destroy
    session.delete(:connection_grant)
    redirect_to settings_connections_path, notice: "Disconnected."
  end

  private

  def base_url
    params[:base_url].presence || current_user.connection&.base_url || DEFAULT_BASE_URL
  end

  # Drops a grant that ran out while the tab sat open.
  def pending_grant
    grant = session[:connection_grant]
    return nil if grant.blank?

    if Time.parse(grant["expires_at"]) <= Time.current
      session.delete(:connection_grant)
      return nil
    end

    grant
  rescue ArgumentError
    session.delete(:connection_grant)
    nil
  end

  def store_connection(base_url, token)
    current_user.connection&.destroy
    current_user.create_connection!(
      base_url: base_url,
      token: token["token"],
      scopes: Array(token["scopes"]).join(","),
      token_expires_at: token["expires_at"]
    )
  end
end
