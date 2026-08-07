class Settings::AdminUserActionsController < ApplicationController
  before_action :admin_only
  before_action :set_user

  def approve
    @user.toggle!(:approved)
    redirect_to settings_users_path
  end

  def password_reset
    PasswordsMailer.reset(@user).deliver_now
    redirect_to settings_users_path, notice: "Password reset instructions sent"
  end

  private

  def set_user
    @user = User.find(params[:user_id])
  end
end
