require "test_helper"

class StartPageGroupsControllerTest < ActionDispatch::IntegrationTest
  def setup
    @user = users(:one)
    @start_page = StartPage.create!(user: @user, name: "Test Page", columns: 3)
    sign_in_as(@user)
  end

  test "should create group" do
    assert_difference("StartPageGroup.count") do
      post start_groups_path, params: {
        start_page_group: {
          name: "Work Links",
          column: 1,
          position: 0
        }
      }
    end

    assert_redirected_to edit_start_path
    assert_equal "Group created successfully.", flash[:notice]

    group = StartPageGroup.find_by(name: "Work Links")
    assert_not_nil group
    assert_equal @start_page, group.start_page
    assert_equal 1, group.column
    assert_equal 0, group.position
  end

  test "should not create invalid group" do
    assert_no_difference("StartPageGroup.count") do
      post start_groups_path, params: {
        start_page_group: {
          name: "",
          column: 5,
          position: 0
        }
      }
    end

    assert_redirected_to edit_start_path
    assert_match /Failed to create group/, flash[:alert]
  end

  test "should update group" do
    group = @start_page.start_page_groups.create!(name: "Original Name", column: 1, position: 0)

    patch start_group_path(group), params: {
      start_page_group: {
        name: "Updated Name"
      }
    }

    assert_redirected_to edit_start_path
    assert_equal "Group updated successfully.", flash[:notice]

    group.reload
    assert_equal "Updated Name", group.name
  end

  test "should not update with invalid data" do
    group = @start_page.start_page_groups.create!(name: "Valid Name", column: 1, position: 0)

    patch start_group_path(group), params: {
      start_page_group: {
        name: ""
      }
    }

    assert_redirected_to edit_start_path
    assert_match /Failed to update group/, flash[:alert]

    group.reload
    assert_equal "Valid Name", group.name
  end

  test "should destroy group" do
    group = @start_page.start_page_groups.create!(name: "Test Group", column: 1, position: 0)

    assert_difference("StartPageGroup.count", -1) do
      delete start_group_path(group)
    end

    assert_redirected_to edit_start_path
    assert_equal "Group deleted successfully.", flash[:notice]
  end

  test "should move group to different column" do
    group = @start_page.start_page_groups.create!(name: "Test Group", column: 1, position: 0)

    post move_start_group_path(group), params: { column: 2, position: 1 }

    assert_redirected_to edit_start_path
    assert_equal "Group moved successfully.", flash[:notice]

    group.reload
    assert_equal 2, group.column
    assert_equal 1, group.position
  end

  test "should not move group to invalid column" do
    group = @start_page.start_page_groups.create!(name: "Test Group", column: 1, position: 0)

    post move_start_group_path(group), params: { column: 5, position: 0 }

    assert_redirected_to edit_start_path
    assert_equal "Failed to move group.", flash[:alert]

    group.reload
    assert_equal 1, group.column
  end

  test "should redirect if no start page exists" do
    @start_page.destroy

    post start_groups_path, params: {
      start_page_group: {
        name: "Test Group",
        column: 1,
        position: 0
      }
    }

    assert_redirected_to settings_start_page_path
  end

  test "should require authentication" do
    sign_out

    post start_groups_path, params: {
      start_page_group: {
        name: "Test Group"
      }
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
