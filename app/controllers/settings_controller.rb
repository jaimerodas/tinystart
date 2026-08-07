class SettingsController < ApplicationController
  def show
  end

  def update
    if current_user.update(user_params)
      redirect_to settings_path, notice: "Settings updated successfully."
    else
      redirect_to settings_path, alert: "Failed to update settings."
    end
  end

  private

  def user_params
    params.require(:user).permit(:theme_preference, :color_preference, :columns)
  end
end
