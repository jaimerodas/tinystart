module StartPageHelper
  def group_move_buttons(group, groups_in_column, column_number)
    create_move_buttons(
      item: group,
      collection: groups_in_column,
      up_params: { column: column_number, position: group.position - 1 },
      down_params: { column: column_number, position: group.position + 1 },
      path_method: :move_start_group_path,
      item_type: "group"
    )
  end

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
      link_to("Settings", settings_start_page_path, class: "button-link")
    ]
  end

  def start_page_show_actions
    [
      link_to("Edit", edit_start_path, class: "button-link"),
      link_to("Settings", settings_path, class: "button-link")
    ]
  end

  def item_move_buttons(item, group)
    create_move_buttons(
      item: item,
      collection: group.ordered_items,
      up_params: { position: item.position - 1 },
      down_params: { position: item.position + 1 },
      path_method: :move_start_item_path,
      item_type: "item"
    )
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

  def group_name_form(group)
    form_with model: group, url: start_group_path(group), method: :patch, local: true, class: "group-name-form" do |form|
      content_tag(:div, class: "group-name-input") do
        concat(form.text_field(:name, value: group.name, required: true, class: "group-name-field"))
        concat(form.submit("Save", class: "save-group-name"))
      end
    end
  end

  private

  def create_move_buttons(item:, collection:, up_params:, down_params:, path_method:, item_type:)
    buttons = []
    item_index = collection.index(item)

    # Move up button (unless first in collection)
    unless item_index == 0
      buttons << button_to(send(path_method, item),
                          method: :post,
                          params: up_params,
                          title: "Move #{item_type} up") { icon("up") }
    end

    # Move down button (unless last in collection)
    unless item_index == collection.count - 1
      buttons << button_to(send(path_method, item),
                          method: :post,
                          params: down_params,
                          title: "Move #{item_type} down") { icon("down") }
    end

    safe_join(buttons)
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
