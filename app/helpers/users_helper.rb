module UsersHelper
  def user_status(user)
    status = user.approved? ? "approved" : "blocked"
    content_tag :span, status.capitalize, class: "user-status #{status}"
  end

  def status_toggle(user)
    return if user.approved? && user.admin?
    text = user.approved? ? "Block" : "Approve"
    button_to(text, settings_user_approve_path(user))
  end

  def reset_password(user)
    return if @current_user == user
    button_to("Reset password", settings_user_password_reset_path(user))
  end
end
