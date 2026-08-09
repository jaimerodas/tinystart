module ApplicationHelper
  # Every icon in the app sits inside a control that carries its own label, so
  # the glyph is decorative in all of them and an unmarked <svg> only adds noise
  # to what a screen reader reads out. Marking it here rather than in the files
  # means a new icon can't forget to.
  def icon(name)
    file_path = Rails.root.join("app", "assets", "icons", "#{name}.svg")
    return unless File.exist?(file_path)
    File.read(file_path).sub("<svg", %(<svg aria-hidden="true" focusable="false")).html_safe
  end

  # Text, not icons. The menu holds two items at most, which fits on the
  # narrowest phone, so there is nothing to compact away behind glyphs.
  def main_menu_link(path, title)
    link_to title, path, class: "main-menu-link"
  end

  # Turbo is off on purpose: logging out has to reload the document so the
  # theme and color attributes on <html> fall back to the logged-out defaults.
  def main_menu_logout_button
    button_to "Log out", session_path, method: :delete, form: { data: { turbo: false } }
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
