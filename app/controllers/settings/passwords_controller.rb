class Settings::PasswordsController < ApplicationController
  before_action :set_user

  def edit
  end

  def update
    respond_to do |format|
      if @user.update_password(user_params)
        format.html { redirect_to settings_path, notice: "Password was successfully changed." }
        format.json { render :edit, status: :ok }
      else
        format.html { render :edit, status: :unprocessable_content }
        format.json { render json: @user.errors, status: :unprocessable_content }
      end
    end
  end

  private

  def set_user
    @user = current_user
  end

  def user_params
    params.expect(user: [ :existing_password, :new_password ])
  end
end
