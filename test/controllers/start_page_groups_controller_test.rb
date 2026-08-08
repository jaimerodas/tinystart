require "test_helper"

class StartPageGroupsControllerTest < ActionDispatch::IntegrationTest
  def setup
    @user = users(:one)
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
    assert_equal @user, group.user
    assert_equal 1, group.column
    assert_equal 0, group.position
  end

  # The add-group form sits at the bottom of a column and sends no position,
  # so a new group has to land after the ones already there.
  test "should append a new group to the end of its column" do
    @user.start_page_groups.create!(name: "First", column: 2, position: 0)
    @user.start_page_groups.create!(name: "Second", column: 2, position: 1)

    post start_groups_path, params: { start_page_group: { name: "Third", column: 2 } }

    assert_equal 2, StartPageGroup.find_by(name: "Third").position
  end

  test "should ignore a position sent by hand" do
    @user.start_page_groups.create!(name: "First", column: 1, position: 0)

    post start_groups_path, params: {
      start_page_group: { name: "Second", column: 1, position: 0 }
    }

    assert_equal 1, StartPageGroup.find_by(name: "Second").position
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
    group = @user.start_page_groups.create!(name: "Original Name", column: 1, position: 0)

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
    group = @user.start_page_groups.create!(name: "Valid Name", column: 1, position: 0)

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
    group = @user.start_page_groups.create!(name: "Test Group", column: 1, position: 0)

    assert_difference("StartPageGroup.count", -1) do
      delete start_group_path(group)
    end

    assert_redirected_to edit_start_path
    assert_equal "Group deleted successfully.", flash[:notice]
  end

  test "should compact the column after destroying a group from the middle" do
    first = @user.start_page_groups.create!(name: "First", column: 1, position: 0)
    middle = @user.start_page_groups.create!(name: "Middle", column: 1, position: 1)
    last = @user.start_page_groups.create!(name: "Last", column: 1, position: 2)

    delete start_group_path(middle)

    assert_equal 0, first.reload.position
    assert_equal 1, last.reload.position
  end

  # --- turbo stream responses ---
  #
  # Every write is scoped to the smallest node that can have changed, so the
  # rest of the page — including any other form someone has open — stays put.

  test "should replace only the column when a turbo stream creates a group" do
    post start_groups_path,
         params: { start_page_group: { name: "Work", column: 2 } },
         as: :turbo_stream

    assert_response :success
    assert_match %r{<turbo-stream action="replace" target="column_2">}, response.body
    assert_no_match %r{target="start_page_grid"}, response.body
  end

  test "should re-render the form with errors when a turbo stream create fails" do
    @user.start_page_groups.create!(name: "Work", column: 1, position: 0)

    assert_no_difference("StartPageGroup.count") do
      post start_groups_path,
           params: { start_page_group: { name: "Work", column: 2 } },
           as: :turbo_stream
    end

    assert_response :unprocessable_content
    assert_match %r{target="new_group_column_2"}, response.body
    assert_match(/has already been taken/, response.body)
  end

  test "should replace only the group when a turbo stream renames it" do
    group = @user.start_page_groups.create!(name: "Original", column: 1, position: 0)

    patch start_group_path(group),
          params: { start_page_group: { name: "Renamed" } },
          as: :turbo_stream

    assert_response :success
    assert_match %r{<turbo-stream action="replace" target="group_#{group.id}">}, response.body
    assert_equal "Renamed", group.reload.name
  end

  test "should keep the rename form open with its errors when it fails" do
    @user.start_page_groups.create!(name: "Taken", column: 1, position: 0)
    group = @user.start_page_groups.create!(name: "Original", column: 1, position: 1)

    patch start_group_path(group),
          params: { start_page_group: { name: "Taken" } },
          as: :turbo_stream

    assert_response :unprocessable_content
    assert_match %r{target="group_#{group.id}"}, response.body
    assert_match(/has already been taken/, response.body)
    assert_equal "Original", group.reload.name
  end

  test "should replace the column when a turbo stream destroys a group" do
    group = @user.start_page_groups.create!(name: "Work", column: 3, position: 0)

    delete start_group_path(group), as: :turbo_stream

    assert_response :success
    assert_match %r{<turbo-stream action="replace" target="column_3">}, response.body
  end

  test "should move group to different column" do
    @user.start_page_groups.create!(name: "Already there", column: 2, position: 0)
    group = @user.start_page_groups.create!(name: "Test Group", column: 1, position: 0)

    post move_start_group_path(group), params: { column: 2, position: 1 }

    assert_redirected_to edit_start_path
    assert_equal "Group moved successfully.", flash[:notice]

    group.reload
    assert_equal 2, group.column
    assert_equal 1, group.position
  end

  # A drag within one column is the case the move buttons never produced: the
  # target position is always already occupied.
  test "should reorder a group within its own column" do
    first = @user.start_page_groups.create!(name: "First", column: 1, position: 0)
    second = @user.start_page_groups.create!(name: "Second", column: 1, position: 1)
    third = @user.start_page_groups.create!(name: "Third", column: 1, position: 2)

    post move_start_group_path(third), params: { column: 1, position: 0 }

    assert_redirected_to edit_start_path
    assert_equal [ "Third", "First", "Second" ], @user.groups_in_column(1).map(&:name)
    assert_equal [ 0, 1, 2 ], [ third, first, second ].map { |g| g.reload.position }
  end

  test "should not move group to invalid column" do
    group = @user.start_page_groups.create!(name: "Test Group", column: 1, position: 0)

    post move_start_group_path(group), params: { column: 5, position: 0 }

    assert_redirected_to edit_start_path
    assert_equal "Failed to move group.", flash[:alert]

    group.reload
    assert_equal 1, group.column
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
