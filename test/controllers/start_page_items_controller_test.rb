require "test_helper"
require_relative "../support/position_write_failure"

class StartPageItemsControllerTest < ActionDispatch::IntegrationTest
  include PositionWriteFailure

  def setup
    @user = users(:one)
    @start_page = StartPage.create!(user: @user, name: "Test Page", columns: 3)
    @group = @start_page.start_page_groups.create!(name: "Test Group", column: 1, position: 0)
    @item_url = "https://example.com/one"
    sign_in_as(@user)
  end

  test "should create item" do
    assert_difference("StartPageItem.count") do
      post start_items_path, params: {
        start_page_item: { url: @item_url, title: "One" },
        group_id: @group.id
      }
    end

    assert_redirected_to edit_start_path
    assert_equal "Tile added.", flash[:notice]

    item = StartPageItem.find_by(url: "https://example.com/one", title: "One", start_page_group: @group)
    assert_not_nil item
    assert_equal 0, item.position
  end

  test "should not create duplicate item in same group" do
    @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)

    assert_no_difference("StartPageItem.count") do
      post start_items_path, params: {
        start_page_item: { url: @item_url, title: "One" },
        group_id: @group.id
      }
    end

    assert_redirected_to edit_start_path
    assert_match(/Failed to add tile/, flash[:alert])
  end

  test "should destroy item" do
    item = @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)

    assert_difference("StartPageItem.count", -1) do
      delete start_item_path(item)
    end

    assert_redirected_to edit_start_path
    assert_equal "Tile removed.", flash[:notice]
  end

  test "should compact remaining positions after destroying an item from the middle" do
    first = @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)
    middle = @group.start_page_items.create!(url: "https://example.com/two", title: "Two", position: 1)
    last = @group.start_page_items.create!(url: "https://example.com/three", title: "Three", position: 2)

    delete start_item_path(middle)

    assert_redirected_to edit_start_path
    assert_equal 0, first.reload.position
    assert_equal 1, last.reload.position
  end

  test "should keep the item when closing the gap fails" do
    @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)
    middle = @group.start_page_items.create!(url: "https://example.com/two", title: "Two", position: 1)
    last = @group.start_page_items.create!(url: "https://example.com/three", title: "Three", position: 2)

    # Only the last item moves, from 2 to 1
    failing_position_write(1) do
      assert_raises(ActiveRecord::StatementInvalid) do
        delete start_item_path(middle)
      end
    end

    assert StartPageItem.exists?(middle.id)
    assert_equal 2, last.reload.position
  end

  test "should move item to different group" do
    item = @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)
    new_group = @start_page.start_page_groups.create!(name: "New Group", column: 2, position: 0)

    post move_start_item_path(item), params: {
      group_id: new_group.id,
      position: 1
    }

    assert_redirected_to edit_start_path
    assert_equal "Item moved successfully.", flash[:notice]

    item.reload
    assert_equal new_group, item.start_page_group
  end

  test "should move item to different position in same group" do
    item1 = @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)
    item2 = @group.start_page_items.create!(url: "https://example.com/two", title: "Two", position: 1)

    post move_start_item_path(item1), params: { position: 1 }

    assert_redirected_to edit_start_path
    assert_equal "Item moved successfully.", flash[:notice]

    item1.reload
    assert_equal 1, item1.position
  end

  test "should report failure when moving an item into a group that already has the link" do
    item = @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)
    new_group = @start_page.start_page_groups.create!(name: "New Group", column: 2, position: 0)
    new_group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)

    post move_start_item_path(item), params: { group_id: new_group.id, position: 0 }

    assert_redirected_to edit_start_path
    assert_equal "Failed to move item.", flash[:alert]
    assert_equal @group, item.reload.start_page_group
  end

  test "should return 422 when a JSON move fails" do
    item = @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)
    new_group = @start_page.start_page_groups.create!(name: "New Group", column: 2, position: 0)
    new_group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)

    post move_start_item_path(item, format: :json), params: { group_id: new_group.id, position: 0 }

    assert_response :unprocessable_content
    assert_equal "error", response.parsed_body["status"]
    assert_equal @group, item.reload.start_page_group
  end

  test "should return success json when a JSON move succeeds" do
    item = @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)
    new_group = @start_page.start_page_groups.create!(name: "New Group", column: 2, position: 0)

    post move_start_item_path(item, format: :json), params: { group_id: new_group.id, position: 0 }

    assert_response :success
    assert_equal "success", response.parsed_body["status"]
    assert_equal new_group, item.reload.start_page_group
  end

  test "should not allow access to other users items" do
    other_user = users(:two)
    other_start_page = StartPage.create!(user: other_user, name: "Other Page", columns: 3)
    other_group = other_start_page.start_page_groups.create!(name: "Other Group", column: 1, position: 0)
    other_item = other_group.start_page_items.create!(url: "https://example.com/two", title: "Two", position: 0)

    # Should get 404 when trying to access other user's item
    delete start_item_path(other_item)
    assert_response :not_found
  end

  test "should redirect if no start page exists" do
    @start_page.destroy

    post start_items_path, params: {
      start_page_item: { url: @item_url, title: "One" },
      group_id: @group.id
    }

    assert_redirected_to settings_start_page_path
  end

  test "should require authentication" do
    sign_out

    post start_items_path, params: {
      start_page_item: { url: @item_url, title: "One" },
      group_id: @group.id
    }

    assert_redirected_to new_session_path
  end

  private

  def sign_in_as(user)
    post session_url, params: { email: user[:email], password: "password123" }
  end

  def sign_out
    delete session_path
  end
end
