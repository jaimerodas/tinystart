class Settings::UsersController < ApplicationController
  before_action :admin_only

  def index
    @users = User.all.order(created_at: :desc)
  end
end
