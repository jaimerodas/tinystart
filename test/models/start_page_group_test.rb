require "test_helper"
require_relative "../support/position_write_failure"

class StartPageGroupTest < ActiveSupport::TestCase
  include PositionWriteFailure

  def setup
    @user = users(:one)
    @group = StartPageGroup.new(
      user: @user,
      name: "Test Group",
      column: 1,
      position: 0
    )
  end

  test "should be valid with valid attributes" do
    assert @group.valid?
  end

  test "should require user" do
    @group.user = nil
    assert_not @group.valid?
    assert_includes @group.errors[:user], "must exist"
  end

  test "should require name" do
    @group.name = nil
    assert_not @group.valid?
    assert_includes @group.errors[:name], "can't be blank"
  end

  test "should require unique name within user" do
    @group.save!

    duplicate = StartPageGroup.new(
      user: @user,
      name: "Test Group",
      column: 2,
      position: 0
    )

    assert_not duplicate.valid?
    assert_includes duplicate.errors[:name], "has already been taken"
  end

  test "should allow same name across different users" do
    @group.save!

    other_group = StartPageGroup.new(
      user: users(:two),
      name: "Test Group",
      column: 1,
      position: 0
    )

    assert other_group.valid?
  end

  test "should require column" do
    @group.column = nil
    assert_not @group.valid?
    assert_includes @group.errors[:column], "can't be blank"
  end

  # A new group gets its position from place_at_end_of_column, so the presence
  # validation is here to stop an existing one from losing it.
  test "should require position on an existing group" do
    @group.save!
    @group.position = nil

    assert_not @group.valid?
    assert_includes @group.errors[:position], "can't be blank"
  end

  test "should validate column within the user column limit" do
    @group.column = 4
    assert_not @group.valid?
    assert_includes @group.errors[:column], "cannot exceed start page column limit of 3"
  end

  test "should move to different column" do
    @group.save!

    success = @group.move_to_column(2)

    assert success
    assert_equal 2, @group.column
  end

  test "should not move to invalid column" do
    @group.save!

    success = @group.move_to_column(5)

    assert_not success
  end

  # move_to_column renumbers with update_column, which skips the numericality
  # validation that used to catch this. A group parked outside column_range
  # renders nowhere and there is no UI left to bring it back.
  test "should not move to a column below the first one" do
    @group.save!

    [ 0, -3 ].each do |bad_column|
      assert_not @group.move_to_column(bad_column, 0), "expected #{bad_column} to be refused"
      assert_equal 1, @group.reload.column
    end
  end

  test "should not move to a column that is not a number" do
    @group.save!

    assert_not @group.move_to_column(nil, 0)
    assert_equal 1, @group.reload.column
  end

  test "should move after the groups already in the target column" do
    @group.save!
    @user.start_page_groups.create!(name: "First", column: 2, position: 0)
    @user.start_page_groups.create!(name: "Second", column: 2, position: 1)

    success = @group.move_to_column(2)

    assert success
    assert_equal 2, @group.reload.position
  end

  # --- move_to_column: ordering ---
  #
  # Dropping a group anywhere in a column means the position it lands on is
  # usually already taken. Writing it without shifting the neighbours leaves two
  # groups sharing a position, and `ordered` breaks that tie arbitrarily.

  test "should renumber the column when a group moves up within it" do
    first = @user.start_page_groups.create!(name: "First", column: 1, position: 0)
    second = @user.start_page_groups.create!(name: "Second", column: 1, position: 1)
    third = @user.start_page_groups.create!(name: "Third", column: 1, position: 2)

    assert third.move_to_column(1, 0)

    assert_equal [ "Third", "First", "Second" ], @user.groups_in_column(1).map(&:name)
    assert_equal [ 0, 1, 2 ], [ third, first, second ].map { |g| g.reload.position }
  end

  test "should renumber the column when a group moves down within it" do
    first = @user.start_page_groups.create!(name: "First", column: 1, position: 0)
    second = @user.start_page_groups.create!(name: "Second", column: 1, position: 1)
    third = @user.start_page_groups.create!(name: "Third", column: 1, position: 2)

    assert first.move_to_column(1, 2)

    assert_equal [ "Second", "Third", "First" ], @user.groups_in_column(1).map(&:name)
    assert_equal [ 0, 1, 2 ], [ second, third, first ].map { |g| g.reload.position }
  end

  test "should insert between the groups already in the target column" do
    @group.save!
    first = @user.start_page_groups.create!(name: "First", column: 2, position: 0)
    second = @user.start_page_groups.create!(name: "Second", column: 2, position: 1)

    assert @group.move_to_column(2, 1)

    assert_equal [ "First", "Test Group", "Second" ], @user.groups_in_column(2).map(&:name)
    assert_equal [ 0, 1, 2 ], [ first, @group, second ].map { |g| g.reload.position }
  end

  test "should close the gap in the column a group left behind" do
    @group.save!
    stays = @user.start_page_groups.create!(name: "Stays", column: 1, position: 1)

    assert @group.move_to_column(2, 0)

    assert_equal 0, stays.reload.position
  end

  test "should clamp a position past the end of the column" do
    first = @user.start_page_groups.create!(name: "First", column: 2, position: 0)
    @group.save!

    assert @group.move_to_column(2, 99)

    assert_equal [ "First", "Test Group" ], @user.groups_in_column(2).map(&:name)
    assert_equal [ 0, 1 ], [ first, @group ].map { |g| g.reload.position }
  end

  test "should not move an item that belongs to another group" do
    @group.save!
    other_group = @user.start_page_groups.create!(name: "Other Group", column: 2, position: 0)
    foreign_item = other_group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)

    success = @group.move_item_to_position(foreign_item, 0)

    assert_not success
    assert_equal 0, foreign_item.reload.position
  end

  test "should not move an item another request already deleted" do
    @group.save!
    item = @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)
    StartPageItem.where(id: item.id).delete_all

    success = @group.move_item_to_position(item, 0)

    assert_not success
  end

  test "should not move an item to a position it cannot compare" do
    @group.save!
    item = @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)

    success = @group.move_item_to_position(item, "first")

    assert_not success
    assert_equal 0, item.reload.position
  end

  test "should roll back every position when a write fails midway" do
    @group.save!
    first = @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)
    second = @group.start_page_items.create!(url: "https://example.com/two", title: "Two", position: 1)
    third = @group.start_page_items.create!(url: "https://example.com/three", title: "Three", position: 2)

    # Moving third to the front rewrites positions as 0, then 1, then 2
    failing_position_write(2) do
      assert_raises(ActiveRecord::StatementInvalid) do
        @group.move_item_to_position(third, 0)
      end
    end

    assert_equal 0, first.reload.position
    assert_equal 1, second.reload.position
    assert_equal 2, third.reload.position
  end

  test "should not rewrite positions that are already correct" do
    @group.save!
    first = @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)
    @group.start_page_items.create!(url: "https://example.com/two", title: "Two", position: 1)
    StartPageItem.any_instance.expects(:update_column).never

    assert @group.move_item_to_position(first, 0)
  end

  test "should not swallow database errors while reordering" do
    @group.save!
    first = @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 0)
    @group.start_page_items.create!(url: "https://example.com/two", title: "Two", position: 1)
    StartPageItem.any_instance.stubs(:update_column).raises(ActiveRecord::StatementInvalid, "database is locked")

    assert_raises(ActiveRecord::StatementInvalid) do
      @group.move_item_to_position(first, 1)
    end
  end

  # --- saved_name ---

  test "saved_name is the name" do
    @group.save!

    assert_equal "Test Group", @group.saved_name
  end

  # The rejected name stays in `name` so the reopened form can show what was
  # typed; everything that labels the group has to ignore it.
  test "saved_name ignores a rename that was not saved" do
    @group.save!
    @group.name = "Rejected"

    assert_equal "Test Group", @group.saved_name
  end

  # The add-group form lives at the bottom of a column and says nothing about
  # position, so the model has to work out where a new group lands.
  test "should place a new group at the end of its column" do
    @user.start_page_groups.create!(name: "First", column: 1, position: 0)
    @user.start_page_groups.create!(name: "Second", column: 1, position: 1)

    appended = @user.start_page_groups.create!(name: "Third", column: 1)

    assert_equal 2, appended.position
  end

  test "should start the first group in a column at zero" do
    @user.start_page_groups.create!(name: "Elsewhere", column: 2, position: 0)

    first = @user.start_page_groups.create!(name: "Here", column: 1)

    assert_equal 0, first.position
  end

  test "should keep an explicit position when one is given" do
    group = @user.start_page_groups.create!(name: "Pinned", column: 1, position: 4)

    assert_equal 4, group.position
  end

  test "should return ordered items" do
    @group.save!
    item1 = @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 1)
    item2 = @group.start_page_items.create!(url: "https://example.com/two", title: "Two", position: 0)

    ordered_items = @group.ordered_items

    assert_equal [ item2, item1 ], ordered_items.to_a
  end
end
