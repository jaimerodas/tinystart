module ApplicationHelper
  def icon(name)
    file_path = Rails.root.join("app", "assets", "icons", "#{name}.svg")
    return unless File.exist?(file_path)
    File.read(file_path).html_safe
  end

  def main_menu_link(path, title)
    link_to path, class: "main-menu-link" do
      concat icon(title.parameterize(separator: "_"))
      concat content_tag("span", title)
    end
  end

  # Turbo is off on purpose: logging out has to reload the document so the
  # theme and color attributes on <html> fall back to the logged-out defaults.
  def main_menu_logout_button
    button_to session_path, method: :delete, form: { data: { turbo: false } } do
      concat icon("logout")
      concat content_tag("span", "Log out")
    end
  end

  def theme_data_attribute
    return "system" unless current_user
    current_user.theme_preference
  end

  def color_data_attribute
    return "teal" unless current_user
    current_user.color_preference
  end
end
