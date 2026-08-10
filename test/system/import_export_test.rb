require "application_system_test_case"

# The import form in a real browser. The controller test already drives a real
# multipart POST, so what is left here is the half that only exists on the
# client: the confirm that stands between a click and a page being replaced.
class ImportExportTest < ApplicationSystemTestCase
  def setup
    @user = users(:one)
    @user.start_page_groups.destroy_all
    group = @user.start_page_groups.create!(name: "Lo de siempre", column: 1)
    group.start_page_items.create!(title: "Fastmail", url: "https://app.fastmail.com")
    sign_in_as(@user)
  end

  test "importing asks before it replaces the page, and does it when told to" do
    visit settings_import_export_path
    attach_file "file", file_fixture("start_page.yml")

    accept_confirm(/replaces every group and link/i) do
      click_button "Import"
    end

    assert_text(/Imported 6 links/i)
    assert_equal 3, @user.start_page_groups.reload.count
    assert_equal 2, @user.reload.columns
  end

  test "dismissing the confirm leaves the page alone" do
    visit settings_import_export_path
    attach_file "file", file_fixture("start_page.yml")

    dismiss_confirm do
      click_button "Import"
    end

    assert_no_text(/Imported/i)
    assert_equal [ "Lo de siempre" ], @user.start_page_groups.reload.map(&:name)
  end

  # Turbo Drive intercepts link clicks and has nothing to do with an attachment
  # response, so the export link opts out of it. Asserted here rather than left
  # to the controller test, because it is the client half of the download.
  test "the export link opts out of Turbo" do
    visit settings_import_export_path

    assert_selector "a[href='#{settings_export_path}'][data-turbo='false']"
  end

  def sign_in_as(user)
    visit new_session_path
    fill_in "email", with: user[:email]
    fill_in "password", with: "password123"
    click_button "Sign in"
    assert_selector "main.start-page", wait: 10
  end
end
