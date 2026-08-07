class SessionsController < ApplicationController
  allow_unauthenticated_access only: %i[ new create ]
  rate_limit to: 10, within: 3.minutes, only: :create, with: -> { redirect_to new_session_url, alert: "Try again later." }
  before_action :send_empty_install_to_signup, only: %i[ new create ]
  layout "session"

  def new
  end

  def create
    user = User.authenticate_by(params.permit(:email, :password))
    if user && user.approved?
      start_new_session_for user
      redirect_to after_authentication_url
    else
      redirect_to new_session_url, alert: "Try another email address or password."
    end
  end

  def destroy
    terminate_session
    redirect_to new_session_url
  end

  private

  # Nobody to log in as yet, so the form would be a dead end. The first signup
  # bootstraps itself as an approved admin.
  def send_empty_install_to_signup
    redirect_to sign_up_path unless User.exists?
  end
end
