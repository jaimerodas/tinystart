require "test_helper"

class UserTest < ActiveSupport::TestCase
  setup do
    @user = users(:one)
  end

  test "update_password requires existing_password" do
    result = @user.update_password(new_password: "newpass", existing_password: nil)
    refute result
    assert_includes @user.errors[:existing_password], "can't be blank"
  end

  test "update_password requires correct existing_password" do
    result = @user.update_password(new_password: "newpass", existing_password: "wrongpass")
    refute result
    assert_includes @user.errors[:existing_password], "is incorrect"
  end

  test "update_password requires new_password longer than 6" do
    result = @user.update_password(new_password: "short", existing_password: "password123")
    refute result
    assert_includes @user.errors[:new_password], "has to be longer"
  end

  test "update_password succeeds with valid params" do
    old_digest = @user.password_digest
    result = @user.update_password(new_password: "newpassword", existing_password: "password123")
    assert result
    assert @user.authenticate("newpassword")
    refute_equal old_digest, @user.password_digest
  end

  # Email validation tests
  test "should require email" do
    user = User.new(password: "password123")
    refute user.valid?
    assert_includes user.errors[:email], "can't be blank"
  end

  test "should require unique email" do
    user = User.new(email: @user.email, password: "password123")
    refute user.valid?
    assert_includes user.errors[:email], "has already been taken"
  end

  test "should normalize email by stripping and downcasing" do
    user = User.create!(email: "  TEST@EXAMPLE.COM  ", password: "password123")
    assert_equal "test@example.com", user.email
  end

  test "should allow duplicate emails with different cases before normalization" do
    user1 = User.create!(email: "test@example.com", password: "password123")
    user2 = User.new(email: "TEST@EXAMPLE.COM", password: "password123")
    refute user2.valid?
    assert_includes user2.errors[:email], "has already been taken"
  end

  # Authentication tests
  test "should authenticate with correct password" do
    assert @user.authenticate("password123")
  end

  test "should not authenticate with incorrect password" do
    refute @user.authenticate("wrongpassword")
  end

  test "should require password for new users" do
    user = User.new(email: "test@example.com")
    refute user.valid?
    assert_includes user.errors[:password], "can't be blank"
  end

  test "should require password confirmation" do
    user = User.new(email: "test@example.com", password: "password123", password_confirmation: "different")
    refute user.valid?
    assert_includes user.errors[:password_confirmation], "doesn't match Password"
  end

  test "should hash password when saved" do
    user = User.create!(email: "test@example.com", password: "password123")
    refute_equal "password123", user.password_digest
    assert user.password_digest.present?
  end

  # Auth token tests
  # Boolean attribute tests
  test "should default approved to false" do
    user = User.create!(email: "test@example.com", password: "password123")
    refute user.approved?
  end

  test "should default admin to false" do
    user = User.create!(email: "test@example.com", password: "password123")
    refute user.admin?
  end

  test "should allow setting approved to true" do
    user = User.create!(email: "test@example.com", password: "password123", approved: true)
    assert user.approved?
  end

  test "should allow setting admin to true" do
    user = User.create!(email: "test@example.com", password: "password123", admin: true)
    assert user.admin?
  end

  test "should auto-approve the first user" do
    User.destroy_all
    user = User.create!(email: "first@example.com", password: "password123")
    assert user.approved?
  end

  test "should make the first user an admin" do
    User.destroy_all
    user = User.create!(email: "first@example.com", password: "password123")
    assert user.admin?
  end

  test "should not auto-approve or auto-admin subsequent users" do
    User.destroy_all
    User.create!(email: "first@example.com", password: "password123")
    second = User.create!(email: "second@example.com", password: "password123")
    refute second.approved?
    refute second.admin?
  end

  # Association dependency tests
  test "should destroy associated sessions when user is destroyed" do
    session = @user.sessions.create!(user_agent: "test")
    session_id = session.id
    @user.destroy
    refute Session.exists?(session_id)
  end

  # Edge cases and error handling
  test "should handle empty string for email" do
    user = User.new(email: "", password: "password123")
    refute user.valid?
    assert_includes user.errors[:email], "can't be blank"
  end

  test "should handle nil password" do
    user = User.new(email: "test@example.com", password: nil)
    refute user.valid?
    assert_includes user.errors[:password], "can't be blank"
  end

  test "should handle empty string for password" do
    user = User.new(email: "test@example.com", password: "")
    refute user.valid?
    assert_includes user.errors[:password], "can't be blank"
  end

  test "update_password should handle empty new_password" do
    result = @user.update_password(new_password: "", existing_password: "password123")
    refute result
    assert_includes @user.errors[:new_password], "has to be longer"
  end

  test "update_password should handle nil new_password" do
    result = @user.update_password(new_password: nil, existing_password: "password123")
    refute result
    assert_includes @user.errors[:new_password], "has to be longer"
  end

  # Scope and query tests
  test "should find user by email case insensitively" do
    user = User.find_by(email: @user.email.upcase)
    assert_equal @user, user
  end


  # Theme and color preferences.

  test "should default theme_preference to system" do
    user = User.create!(email: "theme@example.com", password: "password123")
    assert_equal "system", user.theme_preference
  end

  test "should default color_preference to teal" do
    user = User.create!(email: "color@example.com", password: "password123")
    assert_equal "teal", user.color_preference
  end

  test "should reject an unknown theme_preference" do
    user = User.new(email: "x@example.com", password: "password123", theme_preference: "neon")
    assert_not user.valid?
    assert_includes user.errors[:theme_preference], "neon is not a valid theme"
  end

  test "should reject an unknown color_preference" do
    user = User.new(email: "x@example.com", password: "password123", color_preference: "chartreuse")
    assert_not user.valid?
    assert_includes user.errors[:color_preference], "chartreuse is not a valid color"
  end

  test "should accept every listed color" do
    User::VALID_COLORS.each do |color|
      user = User.new(email: "#{color}@example.com", password: "password123", color_preference: color)
      assert user.valid?, "#{color} should be a valid color"
    end
  end

  # --- start page ---
  # The start page used to be its own record. It is a column count on the user
  # now, and the grid is built straight off the user's groups.

  # One column, not three: a new grid is empty, and empty columns read as broken.
  test "defaults to a single column" do
    assert_equal 1, User.new.columns
  end

  test "persists the single column default without being told" do
    user = User.create!(email: "fresh@example.com", password: "password123")

    assert_equal 1, user.reload.columns
  end

  test "requires columns" do
    @user.columns = nil
    assert_not @user.valid?
    assert_includes @user.errors[:columns], "can't be blank"
  end

  test "requires columns within one and six" do
    @user.columns = 0
    assert_not @user.valid?
    assert_includes @user.errors[:columns], "must be greater than 0"

    @user.columns = 7
    assert_not @user.valid?
    assert_includes @user.errors[:columns], "must be less than or equal to 6"
  end

  test "column_range spans the configured columns" do
    @user.columns = 4
    assert_equal [ 1, 2, 3, 4 ], @user.column_range
  end

  test "groups_by_column buckets the groups by their column" do
    group1 = @user.start_page_groups.create!(name: "Group 1", column: 1, position: 0)
    group2 = @user.start_page_groups.create!(name: "Group 2", column: 2, position: 0)
    group3 = @user.start_page_groups.create!(name: "Group 3", column: 1, position: 1)

    groups_by_column = @user.groups_by_column

    assert_equal [ group1, group3 ], groups_by_column[1]
    assert_equal [ group2 ], groups_by_column[2]
  end

  test "groups_in_column returns one column in order" do
    group1 = @user.start_page_groups.create!(name: "Group 1", column: 1, position: 1)
    @user.start_page_groups.create!(name: "Group 2", column: 2, position: 0)
    group3 = @user.start_page_groups.create!(name: "Group 3", column: 1, position: 0)

    assert_equal [ group3, group1 ], @user.groups_in_column(1).to_a
  end

  test "links_for_command_bar carries every tile the user owns" do
    search = @user.start_page_groups.create!(name: "Search", column: 1, position: 0)
    development = @user.start_page_groups.create!(name: "Development", column: 2, position: 0)

    amazon = search.start_page_items.create!(url: "https://amazon.com", title: "Amazon Shopping", position: 0)
    development.start_page_items.create!(url: "https://github.com", title: "GitHub", position: 0)
    development.start_page_items.create!(url: "https://stackoverflow.com", title: "Stack Overflow", position: 1)

    links = @user.links_for_command_bar

    assert_equal 3, links.length
    assert_includes links, { title: "Amazon Shopping", url: "https://amazon.com", id: amazon.id }
  end

  # A tinylinks token grants one account; the grid must be just as private.
  test "links_for_command_bar excludes another user's tiles" do
    mine = @user.start_page_groups.create!(name: "Mine", column: 1, position: 0)
    mine.start_page_items.create!(url: "https://mine.example.com", title: "Mine", position: 0)

    theirs = users(:two).start_page_groups.create!(name: "Theirs", column: 1, position: 0)
    theirs.start_page_items.create!(url: "https://theirs.example.com", title: "Theirs", position: 0)

    assert_equal [ "Mine" ], @user.links_for_command_bar.map { |l| l[:title] }
  end

  # Narrowing the grid used to hide any group past the new limit: gone from the
  # start page and gone from the edit page, so its move and delete buttons were
  # unreachable, while its tiles still showed up in the command bar.
  test "refuses to shrink past a column that still holds a group" do
    @user.start_page_groups.create!(name: "Reading", column: 3, position: 0)

    @user.columns = 2

    assert_not @user.valid?
    assert_includes @user.errors[:columns], "can't be fewer than 3 — that would hide \"Reading\". Move them first."
  end

  test "names every group that a shrink would hide" do
    @user.start_page_groups.create!(name: "Reading", column: 3, position: 0)
    @user.start_page_groups.create!(name: "Work", column: 2, position: 0)

    @user.columns = 1

    assert_not @user.valid?
    assert_includes @user.errors[:columns].first, "\"Work\" and \"Reading\""
  end

  test "allows a shrink that strands nothing" do
    @user.start_page_groups.create!(name: "Reading", column: 1, position: 0)

    assert @user.update(columns: 1)
  end

  test "allows widening whatever the groups look like" do
    @user.start_page_groups.create!(name: "Reading", column: 3, position: 0)

    assert @user.update(columns: 6)
  end

  # The check costs a query, so it must not run on unrelated saves.
  test "leaves other updates alone when the column count is untouched" do
    @user.start_page_groups.create!(name: "Reading", column: 3, position: 0)

    assert @user.update(theme_preference: "dark")
  end

  test "destroying a user takes its groups and tiles with it" do
    group = @user.start_page_groups.create!(name: "Work", column: 1, position: 0)
    group.start_page_items.create!(url: "https://example.com", title: "Example", position: 0)

    assert_difference [ "StartPageGroup.count", "StartPageItem.count" ], -1 do
      @user.destroy
    end
  end
end
