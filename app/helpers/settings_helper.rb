module SettingsHelper
  def settings_secondary_nav(active = "Main")
    content_tag :ul, class: "secondary-nav" do
      concat settings_secondary_nav_item("Main", settings_path, active)
      concat settings_secondary_nav_item("Import & Export", settings_import_export_path, active)
      concat settings_secondary_nav_item("Connections", settings_connections_path, active)
      if current_user.admin?
        concat settings_secondary_nav_item("Users", settings_users_path, active)
      end
    end
  end

  def settings_secondary_nav_item(title, path, active)
    classes = (title == active) ? "active" : ""
    content_tag :li do
      link_to(title, path, class: classes)
    end
  end
end
