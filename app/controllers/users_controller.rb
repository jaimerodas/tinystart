class UsersController < ApplicationController
  allow_unauthenticated_access only: %i[new create]

  rate_limit to: 2, within: 5.minutes, only: :create, with: -> { redirect_to new_session_url, alert: "Try again later." }

  layout "session", only: %i[new create]

  # GET /sign_up
  def new
    redirect_to root_path and return if resume_session
    @user = User.new
  end

  # POST /sign_up
  def create
    redirect_to root_path and return if resume_session
    @user = User.new(user_params)

    respond_to do |format|
      if @user.save
        format.html { redirect_to root_path, notice: "User was successfully created." }
      else
        format.html { render :new, status: :unprocessable_content }
        format.json { render json: @user.errors, status: :unprocessable_content }
      end
    end
  end

  private

  # Only allow a list of trusted parameters through.
  def user_params
    params.expect(user: [ :email, :password ])
  end
end
