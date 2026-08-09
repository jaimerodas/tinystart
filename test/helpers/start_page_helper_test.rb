require "test_helper"

class StartPageHelperTest < ActionView::TestCase
  include ApplicationHelper

  setup do
    @user = users(:one)
    @group = @user.start_page_groups.create!(name: "Work", column: 1, position: 0)
    @item = @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)
  end

  # --- start_page_header / actions ---

  test "start_page_header renders show actions by default" do
    self.request.path_info = "/start"
    html = start_page_header

    assert_match %r{<header class="start-page-header">}, html
    assert_match %r{<div class="start-page-actions">}, html
    assert_match %r{href="#{edit_start_path}"}, html
    assert_no_match %r{View Start Page}, html
  end

  test "start_page_header renders edit actions when the path includes edit" do
    self.request.path_info = "/start/edit"
    html = start_page_header

    assert_match %r{href="#{settings_path}"}, html
    assert_match %r{View Start Page}, html
  end

  test "start_page_edit_actions links back to the page and to its settings" do
    actions = start_page_edit_actions
    assert_equal 2, actions.length
    assert_match %r{View Start Page}, actions[0]
    assert_match %r{href="#{root_path}"}, actions[0]
    assert_match %r{href="#{settings_path}"}, actions[1]
  end

  test "start_page_show_actions links to edit and settings" do
    actions = start_page_show_actions
    assert_equal 2, actions.length
    assert_match %r{href="#{edit_start_path}"}, actions[0]
    assert_match %r{Edit}, actions[0]
    assert_match %r{href="#{settings_path}"}, actions[1]
  end

  # --- federation_state ---

  test "federation_state is off without a connection" do
    assert_equal "off", federation_state(nil)
  end

  test "federation_state is active for a healthy connection" do
    connection = @user.create_connection!(base_url: "https://links.example.com", token: "mine")

    assert_equal "active", federation_state(connection)
  end

  test "federation_state is reconnect once the token was rejected" do
    connection = @user.create_connection!(base_url: "https://links.example.com", token: "mine")
    connection.record_failure!("links.example.com rejected the token")

    assert_equal "reconnect", federation_state(connection)
  end

  # --- delete buttons ---

  test "group_delete_button renders a delete form with confirm message" do
    html = group_delete_button(@group)

    assert_match %r{class="remove-button"}, html
    assert_match %r{Delete group}, html
    assert_match %r{data-turbo-confirm="Delete this group and all its tiles\?"}, html
    assert_match %r{action="#{start_group_path(@group)}"}, html
  end

  test "item_delete_button renders a delete form with confirm message" do
    html = item_delete_button(@item)

    assert_match %r{class="remove-button"}, html
    assert_match %r{Remove tile}, html
    assert_match %r{data-turbo-confirm="Remove this tile\?"}, html
    assert_match %r{action="#{start_item_path(@item)}"}, html
  end

  # --- the row buttons leave the tab order ---
  #
  # The row itself is the tab stop, and Enter and Delete reach these by clicking
  # them. Leaving them tabbable would put three stops on every tile, which is
  # the ~100 stops per page the roving highlight exists to remove.

  test "the edit and delete buttons are labelled and out of the tab order" do
    [ group_edit_button, item_edit_button, group_delete_button(@group), item_delete_button(@item) ].each do |html|
      assert_match %r{tabindex="-1"}, html
      assert_match %r{aria-label="}, html
    end
  end

  # --- drag handles ---
  #
  # A pointer affordance only: the row is what a keyboard picks up, so a handle
  # in the accessibility tree would just be a control that does nothing.

  test "group_drag_handle renders a draggable element with group drag actions" do
    html = group_drag_handle

    assert_match %r{class="drag-handle"}, html
    assert_match %r{draggable="true"}, html
    assert_match %r{dragstart-&gt;drag-drop#dragStart}, html
    assert_match %r{data-drag-drop-target="handle"}, html
  end

  test "item_drag_handle renders a draggable element with item drag actions" do
    html = item_drag_handle

    assert_match %r{class="drag-handle"}, html
    assert_match %r{dragstart-&gt;drag-drop#dragItemStart}, html
    assert_match %r{data-drag-drop-target="itemHandle"}, html
  end

  test "drag handles are hidden from assistive technology" do
    assert_match %r{aria-hidden="true"}, group_drag_handle
    assert_match %r{aria-hidden="true"}, item_drag_handle
  end

  # --- edit buttons ---
  #
  # These open a form that is already on the page rather than submitting one,
  # so they are plain buttons, not button_to forms.

  test "group_edit_button renders a plain button that opens the inline form" do
    html = group_edit_button

    assert_match %r{<button}, html
    assert_match %r{type="button"}, html
    assert_match %r{class="edit-button"}, html
    assert_match %r{Rename group}, html
    assert_match %r{click-&gt;inline-form#open}, html
    assert_no_match %r{<form}, html
  end

  test "item_edit_button renders a plain button that opens the inline form" do
    html = item_edit_button

    assert_match %r{type="button"}, html
    assert_match %r{class="edit-button"}, html
    assert_match %r{Edit tile}, html
    assert_match %r{click-&gt;inline-form#open}, html
    assert_no_match %r{<form}, html
  end

  # --- add triggers ---

  # The trigger is the element that hides when the form opens, so it carries
  # the target itself. An edit button only fires the action — the row it sits
  # in is what gets replaced.
  test "add_group_trigger names the column it will add to and is the trigger" do
    html = add_group_trigger(2)

    assert_match %r{class="add-trigger"}, html
    assert_match %r{Add group to column 2}, html
    assert_match %r{click-&gt;inline-form#open}, html
    assert_match %r{data-inline-form-target="trigger"}, html
  end

  test "add_item_trigger names the group it will add to" do
    html = add_item_trigger(@group)

    assert_match %r{class="add-trigger"}, html
    assert_match %r{Add link to Work}, html
    assert_match %r{click-&gt;inline-form#open}, html
  end

  # WCAG 2.5.3: these are the only controls in the editor with visible text, so
  # the name a speech-input user says has to start what the name announces.
  test "an add trigger's accessible name begins with its visible label" do
    { "Add group" => add_group_trigger(2), "Add link" => add_item_trigger(@group) }.each do |label, html|
      assert_match %r{<span>#{label}</span>}, html
      assert_match %r{aria-label="#{label} }, html
    end
  end

  # Unlike the edit and delete buttons, an add trigger is a row in its own right
  # — arrowing past the last tile in a group lands on it — so it stays reachable
  # and joins the roving list.
  test "add triggers are rows in the roving list" do
    [ add_group_trigger(2), add_item_trigger(@group) ].each do |html|
      assert_match %r{tabindex="-1"}, html
      assert_match %r{data-grid-keyboard-target="row"}, html
    end
  end
end
