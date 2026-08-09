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

  # A new tile gets its position from place_at_end_of_group, so the presence
  # validation is here to stop an existing one from losing it.
  test "should require position on an existing tile" do
    @item.save!
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

  # --- move_to_group: ordering ---
  #
  # A drag drops a tile at a chosen slot in the target group, so the position it
  # lands on is usually already taken. Writing it without shifting the
  # neighbours leaves two tiles sharing a position and the tie resolves
  # arbitrarily — the same defect StartPageGroup#move_to_column had.

  test "should insert between the tiles already in the target group" do
    target = @user.start_page_groups.create!(name: "Target", column: 1, position: 1)
    x = target.start_page_items.create!(url: "https://example.com/x", title: "X", position: 0)
    y = target.start_page_items.create!(url: "https://example.com/y", title: "Y", position: 1)
    z = target.start_page_items.create!(url: "https://example.com/z", title: "Z", position: 2)
    travelling = @group.start_page_items.create!(url: "https://example.com/w", title: "W", position: 0)

    assert travelling.move_to_group(target, 1)

    assert_equal [ "X", "W", "Y", "Z" ], target.reload.ordered_items.map(&:title)
    assert_equal [ 0, 1, 2, 3 ], [ x, travelling, y, z ].map { |i| i.reload.position }
  end

  test "should drop a tile at the very top of the target group" do
    target = @user.start_page_groups.create!(name: "Target", column: 1, position: 1)
    target.start_page_items.create!(url: "https://example.com/x", title: "X", position: 0)
    travelling = @group.start_page_items.create!(url: "https://example.com/w", title: "W", position: 0)

    assert travelling.move_to_group(target, 0)

    assert_equal [ "W", "X" ], target.reload.ordered_items.map(&:title)
  end

  test "should append to the target group when no position is given" do
    target = @user.start_page_groups.create!(name: "Target", column: 1, position: 1)
    target.start_page_items.create!(url: "https://example.com/x", title: "X", position: 0)
    travelling = @group.start_page_items.create!(url: "https://example.com/w", title: "W", position: 0)

    assert travelling.move_to_group(target)

    assert_equal [ "X", "W" ], target.reload.ordered_items.map(&:title)
  end

  # The group a tile leaves has to close up behind it, or the next tile added
  # there takes a position that is already in use.
  test "should close the gap in the group a tile left behind" do
    target = @user.start_page_groups.create!(name: "Target", column: 1, position: 1)
    @group.start_page_items.create!(url: "https://example.com/a", title: "A", position: 0)
    middle = @group.start_page_items.create!(url: "https://example.com/b", title: "B", position: 1)
    last = @group.start_page_items.create!(url: "https://example.com/c", title: "C", position: 2)

    assert middle.move_to_group(target, 0)

    assert_equal [ 0, 1 ], @group.reload.ordered_items.map(&:position)
    assert_equal 1, last.reload.position
  end

  test "should still refuse a group that already holds the url" do
    target = @user.start_page_groups.create!(name: "Target", column: 1, position: 1)
    target.start_page_items.create!(url: "https://example.com/w", title: "Mine", position: 0)
    travelling = @group.start_page_items.create!(url: "https://example.com/w", title: "W", position: 0)

    assert_not travelling.move_to_group(target, 0)
    assert_equal @group, travelling.reload.start_page_group
    assert_equal [ "Mine" ], target.reload.ordered_items.map(&:title)
  end

  # --- saved_title ---

  test "saved_title ignores an edit that was not saved" do
    @item.save!
    @item.title = "Rejected"

    assert_equal "One", @item.saved_title
  end

  # --- placement on create ---

  test "should place a new tile at the end of its group" do
    @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)
    @group.start_page_items.create!(url: "https://example.com/two", title: "Two", position: 1)

    appended = @group.start_page_items.create!(url: "https://example.com/three", title: "Three")

    assert_equal 2, appended.position
  end

  # Counting the tiles is not the same as asking where the last one is.
  test "should place a new tile after a gap rather than on top of it" do
    @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)
    @group.start_page_items.create!(url: "https://example.com/two", title: "Two", position: 5)

    appended = @group.start_page_items.create!(url: "https://example.com/three", title: "Three")

    assert_equal 6, appended.position
  end
end
