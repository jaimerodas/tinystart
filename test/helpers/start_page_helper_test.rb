require "test_helper"

class StartPageHelperTest < ActionView::TestCase
  include ApplicationHelper

  setup do
    @user = users(:one)
    @group = @user.start_page_groups.create!(name: "Work", column: 1, position: 0)
    @other_group = @user.start_page_groups.create!(name: "Play", column: 1, position: 1)
    @last_group = @user.start_page_groups.create!(name: "Later", column: 1, position: 2)
    @item = @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)
    @item_two = @group.start_page_items.create!(url: "https://example.com/two", title: "Two", position: 1)
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

  # --- tinylinks_federation_state ---

  test "tinylinks_federation_state is off without a connection" do
    assert_equal "off", tinylinks_federation_state(nil)
  end

  test "tinylinks_federation_state is active for a healthy connection" do
    connection = @user.create_tinylinks_connection!(base_url: "https://links.example.com", token: "mine")

    assert_equal "active", tinylinks_federation_state(connection)
  end

  test "tinylinks_federation_state is reconnect once the token was rejected" do
    connection = @user.create_tinylinks_connection!(base_url: "https://links.example.com", token: "mine")
    connection.record_failure!("tinylinks rejected the token")

    assert_equal "reconnect", tinylinks_federation_state(connection)
  end

  # --- group_move_buttons ---

  test "group_move_buttons renders only down button for first group" do
    groups = @user.start_page_groups.in_column(1).ordered.to_a
    html = group_move_buttons(@group, groups, 1)

    assert_match %r{Move group down}, html
    assert_no_match %r{Move group up}, html
  end

  test "group_move_buttons renders only up button for last group" do
    groups = @user.start_page_groups.in_column(1).ordered.to_a
    html = group_move_buttons(@last_group, groups, 1)

    assert_match %r{Move group up}, html
    assert_no_match %r{Move group down}, html
  end

  test "group_move_buttons renders both buttons for a middle group" do
    groups = @user.start_page_groups.in_column(1).ordered.to_a
    html = group_move_buttons(@other_group, groups, 1)

    assert_match %r{Move group up}, html
    assert_match %r{Move group down}, html
  end

  # --- item_move_buttons ---

  test "item_move_buttons renders only down button for first item" do
    html = item_move_buttons(@item, @group)

    assert_match %r{Move item down}, html
    assert_no_match %r{Move item up}, html
  end

  test "item_move_buttons renders only up button for last item" do
    html = item_move_buttons(@item_two, @group)

    assert_match %r{Move item up}, html
    assert_no_match %r{Move item down}, html
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

  # --- drag handles ---

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

  # --- group_name_form ---

  test "group_name_form renders a patch form for updating the group name" do
    html = group_name_form(@group)

    assert_match %r{class="group-name-form"}, html
    assert_match %r{action="#{start_group_path(@group)}"}, html
    assert_match %r{value="#{@group.name}"}, html
    assert_match %r{name="_method" value="patch"}, html
  end
end
