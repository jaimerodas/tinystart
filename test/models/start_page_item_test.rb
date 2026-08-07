require "test_helper"
require_relative "../support/position_write_failure"

class StartPageItemTest < ActiveSupport::TestCase
  include PositionWriteFailure

  def setup
    @user = users(:one)
    @group = @user.start_page_groups.create!(name: "Test Group", column: 1, position: 0)
    @item = StartPageItem.new(
      start_page_group: @group,
      url: "https://example.com/one",
      title: "One",
      position: 0
    )
  end

  test "should be valid with valid attributes" do
    assert @item.valid?
  end

  test "should require start_page_group" do
    @item.start_page_group = nil
    assert_not @item.valid?
    assert_includes @item.errors[:start_page_group], "must exist"
  end

  test "should require url" do
    @item.url = nil
    assert_not @item.valid?
    assert_includes @item.errors[:url], "can't be blank"
  end

  test "should require title" do
    @item.title = nil
    assert_not @item.valid?
    assert_includes @item.errors[:title], "can't be blank"
  end

  test "should reject a url that is not http or https" do
    @item.url = "ftp://example.com/thing"
    assert_not @item.valid?
    assert_includes @item.errors[:url], "must be a valid URL"
  end

  test "should reject a malformed url" do
    @item.url = "not a url at all"
    assert_not @item.valid?
    assert_includes @item.errors[:url], "must be a valid URL"
  end

  test "should require position" do
    @item.position = nil
    assert_not @item.valid?
    assert_includes @item.errors[:position], "can't be blank"
  end

  test "should enforce unique url per group" do
    @item.save!

    duplicate = StartPageItem.new(
      start_page_group: @group,
      url: @item.url,
      title: "A different title, same destination",
      position: 1
    )

    assert_not duplicate.valid?
    assert_includes duplicate.errors[:url], "has already been taken"
  end

  test "should allow the same url in different groups" do
    @item.save!

    other_group = @user.start_page_groups.create!(name: "Other Group", column: 2, position: 0)

    other_item = StartPageItem.new(
      start_page_group: other_group,
      url: @item.url,
      title: "One",
      position: 0
    )

    assert other_item.valid?
  end

  test "should move to different group" do
    @item.save!

    new_group = @user.start_page_groups.create!(name: "New Group", column: 2, position: 0)

    success = @item.move_to_group(new_group)

    assert success
    assert_equal new_group, @item.start_page_group
  end

  test "should move to specific position" do
    @item.save!

    success = @item.move_to_position(5)

    assert success
    assert_equal 5, @item.position
  end

  test "should not move to negative position" do
    @item.save!

    success = @item.move_to_position(-1)

    assert_not success
  end

  test "should roll back the gap closing when a write fails midway" do
    @item.save!
    second = @group.start_page_items.create!(url: "https://example.com/two", title: "Two", position: 4)
    third = @group.start_page_items.create!(url: "https://example.com/three", title: "Three", position: 9)

    # Closing the gaps rewrites 4 as 1 and 9 as 2
    failing_position_write(2) do
      assert_raises(ActiveRecord::StatementInvalid) do
        @item.reorder_group_positions!
      end
    end

    assert_equal 4, second.reload.position
    assert_equal 9, third.reload.position
  end

  test "should not move to something that is not a group" do
    @item.save!

    success = @item.move_to_group("not a group")

    assert_not success
    assert_equal @group, @item.reload.start_page_group
  end
end
