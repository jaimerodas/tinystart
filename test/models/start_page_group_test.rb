require "test_helper"
require_relative "../support/position_write_failure"

class StartPageGroupTest < ActiveSupport::TestCase
  include PositionWriteFailure

  def setup
    @user = users(:one)
    @start_page = StartPage.create!(user: @user, name: "Test Page", columns: 3)
    @group = StartPageGroup.new(
      start_page: @start_page,
      name: "Test Group",
      column: 1,
      position: 0
    )
  end

  test "should be valid with valid attributes" do
    assert @group.valid?
  end

  test "should require start_page" do
    @group.start_page = nil
    assert_not @group.valid?
    assert_includes @group.errors[:start_page], "must exist"
  end

  test "should require name" do
    @group.name = nil
    assert_not @group.valid?
    assert_includes @group.errors[:name], "can't be blank"
  end

  test "should require unique name within start_page" do
    @group.save!

    duplicate = StartPageGroup.new(
      start_page: @start_page,
      name: "Test Group",
      column: 2,
      position: 0
    )

    assert_not duplicate.valid?
    assert_includes duplicate.errors[:name], "has already been taken"
  end

  test "should allow same name across different start_pages" do
    @group.save!

    other_user = users(:two)
    other_start_page = StartPage.create!(user: other_user, name: "Other Page", columns: 3)

    other_group = StartPageGroup.new(
      start_page: other_start_page,
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

  test "should require position" do
    @group.position = nil
    assert_not @group.valid?
    assert_includes @group.errors[:position], "can't be blank"
  end

  test "should validate column within start_page limit" do
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

  test "should move after the groups already in the target column" do
    @group.save!
    @start_page.start_page_groups.create!(name: "First", column: 2, position: 0)
    @start_page.start_page_groups.create!(name: "Second", column: 2, position: 1)

    success = @group.move_to_column(2)

    assert success
    assert_equal 2, @group.reload.position
  end

  test "should not move an item that belongs to another group" do
    @group.save!
    other_group = @start_page.start_page_groups.create!(name: "Other Group", column: 2, position: 0)
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

  test "should return ordered items" do
    @group.save!
    item1 = @group.start_page_items.create!(url: "https://example.com/one", title: "One", position: 1)
    item2 = @group.start_page_items.create!(url: "https://example.com/two", title: "Two", position: 0)

    ordered_items = @group.ordered_items

    assert_equal [ item2, item1 ], ordered_items.to_a
  end
end
