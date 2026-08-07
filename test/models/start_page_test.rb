require "test_helper"

class StartPageTest < ActiveSupport::TestCase
  def setup
    @user = users(:one)
    @start_page = StartPage.new(
      user: @user,
      name: "My Start Page",
      columns: 3
    )
  end

  test "should be valid with valid attributes" do
    assert @start_page.valid?
  end

  test "should require user" do
    @start_page.user = nil
    assert_not @start_page.valid?
    assert_includes @start_page.errors[:user], "must exist"
  end

  test "should require name" do
    @start_page.name = nil
    assert_not @start_page.valid?
    assert_includes @start_page.errors[:name], "can't be blank"
  end

  test "should require columns" do
    @start_page.columns = nil
    assert_not @start_page.valid?
    assert_includes @start_page.errors[:columns], "can't be blank"
  end

  test "should validate columns range" do
    @start_page.columns = 0
    assert_not @start_page.valid?
    assert_includes @start_page.errors[:columns], "must be greater than 0"

    @start_page.columns = 7
    assert_not @start_page.valid?
    assert_includes @start_page.errors[:columns], "must be less than or equal to 6"
  end

  test "should enforce unique user_id" do
    @start_page.save!

    duplicate = StartPage.new(
      user: @user,
      name: "Another Start Page",
      columns: 2
    )

    assert_not duplicate.valid?
    assert_includes duplicate.errors[:user_id], "has already been taken"
  end

  test "should return groups by column" do
    @start_page.save!

    group1 = @start_page.start_page_groups.create!(name: "Group 1", column: 1, position: 0)
    group2 = @start_page.start_page_groups.create!(name: "Group 2", column: 2, position: 0)
    group3 = @start_page.start_page_groups.create!(name: "Group 3", column: 1, position: 1)

    groups_by_column = @start_page.groups_by_column

    assert_equal [ group1, group3 ], groups_by_column[1]
    assert_equal [ group2 ], groups_by_column[2]
  end

  test "should return column range" do
    @start_page.columns = 4
    assert_equal [ 1, 2, 3, 4 ], @start_page.column_range
  end

  test "should return groups in specific column" do
    @start_page.save!

    group1 = @start_page.start_page_groups.create!(name: "Group 1", column: 1, position: 0)
    group2 = @start_page.start_page_groups.create!(name: "Group 2", column: 2, position: 0)
    group3 = @start_page.start_page_groups.create!(name: "Group 3", column: 1, position: 1)

    column1_groups = @start_page.groups_in_column(1)

    assert_equal [ group1, group3 ], column1_groups.to_a
  end

  test "should return links for command bar" do
    @start_page.save!

    # Create some links with unique URLs (avoiding fixture conflicts)
    group1 = @start_page.start_page_groups.create!(name: "Search", column: 1, position: 0)
    group2 = @start_page.start_page_groups.create!(name: "Development", column: 2, position: 0)

    amazon = group1.start_page_items.create!(url: "https://amazon.com", title: "Amazon Shopping", position: 0)
    group2.start_page_items.create!(url: "https://github.com", title: "GitHub", position: 0)
    group2.start_page_items.create!(url: "https://stackoverflow.com", title: "Stack Overflow", position: 1)

    links_data = @start_page.links_for_command_bar

    assert_equal 3, links_data.length

    amazon_entry = links_data.find { |l| l[:url] == "https://amazon.com" }
    assert_equal "Amazon Shopping", amazon_entry[:title]
    assert_equal amazon.id, amazon_entry[:id]
  end
end
