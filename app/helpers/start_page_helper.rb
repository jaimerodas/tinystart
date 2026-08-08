module StartPageHelper
  def start_page_header
    actions = request.fullpath.include?("edit") ? start_page_edit_actions : start_page_show_actions
    content_tag :header, class: "start-page-header" do
      content_tag(:div, class: "start-page-actions") do
        actions.each { |action| concat(action) }
      end
    end
  end

  def start_page_edit_actions
    [
      link_to("View Start Page", root_path, class: "button-link"),
      link_to("Settings", settings_path, class: "button-link")
    ]
  end

  def start_page_show_actions
    [
      link_to("Edit", edit_start_path, class: "button-link"),
      link_to("Settings", settings_path, class: "button-link")
    ]
  end

  # What the command bar should do about its "All Links" section. Nothing to
  # search means no section at all; a rejected token means a notice rather than
  # a query that will only be rejected again.
  def tinylinks_federation_state(connection)
    return "off" if connection.nil?

    connection.needs_reconnect? ? "reconnect" : "active"
  end

  def group_delete_button(group)
    create_delete_button(
      item: group,
      path: start_group_path(group),
      title: "Delete group",
      confirm_message: "Delete this group and all its tiles?"
    )
  end

  def item_delete_button(item)
    create_delete_button(
      item: item,
      path: start_item_path(item),
      title: "Remove tile",
      confirm_message: "Remove this tile?"
    )
  end

  def group_drag_handle
    create_drag_handle(
      actions: "dragstart->drag-drop#dragStart dragend->drag-drop#dragEnd",
      target: "handle",
      title: "Drag to move group"
    )
  end

  def item_drag_handle
    create_drag_handle(
      actions: "dragstart->drag-drop#dragItemStart dragend->drag-drop#dragItemEnd",
      target: "itemHandle",
      title: "Drag to move item"
    )
  end

  # Both edit buttons open a form that is already on the page rather than
  # submitting one, so they are plain buttons — button_to would wrap them in a
  # form of their own.
  def group_edit_button
    create_edit_button(title: "Rename group")
  end

  def item_edit_button
    create_edit_button(title: "Edit tile")
  end

  def add_group_trigger(column_number)
    create_add_trigger(label: "Add group", title: "Add a group to column #{column_number}")
  end

  def add_item_trigger(group)
    create_add_trigger(label: "Add link", title: "Add a link to #{group.saved_name}")
  end

  # Turbo Stream targets. Every write on the edit page replaces the smallest
  # node that can have changed, so these ids are named in the controllers, the
  # partials and the tests alike — they live here so they only exist once.
  def column_dom_id(column_number)
    "column_#{column_number}"
  end

  def group_dom_id(group)
    "group_#{group.id}"
  end

  def item_dom_id(item)
    "item_#{item.id}"
  end

  def new_group_dom_id(column_number)
    "new_group_column_#{column_number}"
  end

  def new_item_dom_id(group)
    "new_item_group_#{group.id}"
  end

  private

  def create_edit_button(title:)
    content_tag(:button, icon("pencil"),
                type: "button",
                class: "edit-button",
                title: title,
                data: { action: "click->inline-form#open" })
  end

  def create_add_trigger(label:, title:)
    content_tag(:button,
                safe_join([ icon("plus"), content_tag(:span, label) ]),
                type: "button",
                class: "add-trigger",
                title: title,
                data: { action: "click->inline-form#open", inline_form_target: "trigger" })
  end

  def create_delete_button(item:, path:, title:, confirm_message:)
    button_to path, method: :delete,
              class: "remove-button",
              title: title,
              data: { turbo_confirm: confirm_message } do
      icon("xmark")
    end
  end

  def create_drag_handle(actions:, target:, title:)
    content_tag(:div, class: "drag-handle",
                draggable: "true",
                data: { action: actions, drag_drop_target: target },
                title: title) do
      icon("drag-handle")
    end
  end
end
