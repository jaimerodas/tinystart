class SettingsController < ApplicationController
  def show
  end

  def update
    if current_user.update(user_params)
      redirect_to settings_path, notice: "Settings updated successfully."
    else
      redirect_to settings_path,
                  alert: "Failed to update settings: #{current_user.errors.full_messages.join(', ')}"
    end
  end

  private

  def user_params
    # No :columns — StartPagesController#update owns it, so that a refused
    # shrink can answer on the page showing the groups it names.
    params.require(:user).permit(:theme_preference, :color_preference)
  end
end
