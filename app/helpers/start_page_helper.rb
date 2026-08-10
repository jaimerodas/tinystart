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

  # The keyboard shortcuts, as data rather than as markup, because two views
  # render them: the ? dialog on both pages, and the editor's toolbar legend
  # points at it. Each entry is the keys to show and what they do.
  #
  # The page chords are matched on event.code in start_shortcuts_controller, so
  # they are ⌥E and ⌥S whatever the keyboard layout puts under those keys.
  def grid_shortcuts
    [
      [ %w[↑ ↓ ← →], "move between tiles" ],
      [ %w[Home End], "first / last in the column" ],
      [ [ "Space" ], "pick up / drop" ],
      [ [ "Enter" ], "edit" ],
      [ %w[Del ⌫], "delete" ],
      [ [ "Esc" ], "cancel a move" ],
      [ [ "Tab" ], "leave the grid" ]
    ]
  end

  def show_page_shortcuts
    [
      [ [ "⌥", "E" ], "edit the start page" ],
      [ [ "?" ], "show this list" ],
      [ %w[↑ ↓], "move through results" ],
      [ [ "Enter" ], "open" ],
      [ [ "⌘", "Enter" ], "open in a new tab" ],
      [ [ "Esc" ], "clear the bar, then leave it" ]
    ]
  end

  # The grid's keys are only written down here now — the legend stopped
  # listing them when it became a pointer to this list.
  def editor_shortcuts
    [
      [ [ "⌥", "S" ], "back to the start page" ],
      [ [ "?" ], "show this list" ]
    ] + grid_shortcuts
  end

  # What the command bar should do about its "All Links" section. Nothing to
  # search means no section at all; a rejected token means a notice rather than
  # a query that will only be rejected again.
  def federation_state(connection)
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

  # The accessible name has to begin with the visible one. These are the only
  # controls in the editor with visible text, and naming one "Add a group to
  # column 2" while it reads "Add group" means saying "click Add group" matches
  # nothing (WCAG 2.5.3, Label in Name). The column and group still belong in
  # the name — five identical "Add link" buttons on a page name nothing at all.
  def add_group_trigger(column_number)
    create_add_trigger(label: "Add group", title: "Add group to column #{column_number}")
  end

  def add_item_trigger(group)
    create_add_trigger(label: "Add link", title: "Add link to #{group.saved_name}")
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

  # The toolbar's column picker. A refused change replaces it, because it is
  # left showing the value the database would not take.
  def column_count_dom_id
    "column_count"
  end

  # Not a stream target: this one names the group's <section> through
  # aria-labelledby, which is what makes it a region at all.
  def group_name_dom_id(group)
    "group_name_#{group.id}"
  end

  private

  # The row is the tab stop, and Enter reaches this by clicking it, so it leaves
  # the tab order rather than adding a stop to every tile. title is the tooltip;
  # aria-label is the accessible name, which title only ever supplied by
  # fallback.
  def create_edit_button(title:)
    content_tag(:button, icon("pencil"),
                type: "button",
                class: "edit-button",
                title: title,
                tabindex: -1,
                aria: { label: title },
                data: { action: "click->inline-form#open" })
  end

  # Unlike the edit and delete buttons this is a row in its own right — arrowing
  # past the last tile in a group lands on it — so it joins the roving list.
  def create_add_trigger(label:, title:)
    content_tag(:button,
                safe_join([ icon("plus"), content_tag(:span, label) ]),
                type: "button",
                class: "add-trigger",
                title: title,
                tabindex: -1,
                aria: { label: title },
                data: { action: "click->inline-form#open",
                        inline_form_target: "trigger",
                        grid_keyboard_target: "row" })
  end

  def create_delete_button(item:, path:, title:, confirm_message:)
    button_to path, method: :delete,
              class: "remove-button",
              title: title,
              tabindex: -1,
              aria: { label: title },
              data: { turbo_confirm: confirm_message } do
      icon("xmark")
    end
  end

  # A pointer affordance only. The row is what a keyboard picks up, so a handle
  # left in the accessibility tree would just be a control that does nothing.
  def create_drag_handle(actions:, target:, title:)
    content_tag(:div, class: "drag-handle",
                draggable: "true",
                aria: { hidden: "true" },
                data: { action: actions, drag_drop_target: target },
                title: title) do
      icon("drag-handle")
    end
  end
end
