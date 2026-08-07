module Authentication
  extend ActiveSupport::Concern

  included do
    before_action :require_authentication
    helper_method :authenticated?, :current_user
  end

  class_methods do
    def allow_unauthenticated_access(**options)
      skip_before_action :require_authentication, **options
    end
  end

  private
    def current_user
      @current_user ||= Current.session.user
    end

    def authenticated?
      Current.session.present?
    end

    def require_authentication
      resume_session || request_authentication
    end


    def resume_session
      Current.session = find_session_by_cookie
    end

    def find_session_by_cookie
      Session.active.find_by(id: cookies.signed[:session_id])
    end


    def request_authentication
      session[:return_to_after_authenticating] = request.url
      redirect_to new_session_url
    end

    def after_authentication_url
      session.delete(:return_to_after_authenticating) || root_url
    end


    def start_new_session_for(user)
      # Clean up old sessions for this user
      user.sessions.expired.destroy_all

      user.sessions.create!(
        user_agent: request.user_agent,
        ip_address: request.remote_ip,
        expires_at: Session::SESSION_LIFETIME.from_now
      ).tap do |session|
        Current.session = session
        cookies.signed[:session_id] = { value: session.id, httponly: true, same_site: :lax, expires: session.expires_at }
      end
    end

    def terminate_session
      @current_user = false
      Current.session.destroy
      cookies.delete(:session_id)
    end

    def admin_only
      return if current_user.admin?
      redirect_to root_path
    end

    def refresh_session_if_needed
      return unless Current.session

      # Only refresh if session expires within 7 days
      if Current.session.expires_at < 7.days.from_now
        Current.session.extend_session!

        # Sync cookie expiration
        cookies.signed[:session_id] = {
          value: Current.session.id,
          httponly: true,
          same_site: :lax,
          expires: Current.session.expires_at
        }
      end
    end
end
