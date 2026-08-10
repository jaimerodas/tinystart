require "test_helper"

class Settings::ImportExportControllerTest < ActionDispatch::IntegrationTest
  setup do
    @user = users(:one)
    @other = users(:two)
    @user.start_page_groups.destroy_all
  end

  def login_as(user)
    post session_url, params: { email: user.email, password: "password123" }
  end

  def build_page(user = @user)
    group = user.start_page_groups.create!(name: "Lo de siempre", column: 1)
    group.start_page_items.create!(title: "Fastmail", url: "https://app.fastmail.com")
    group
  end

  def upload(body, filename: "start_page.yml")
    Tempfile.open([ "upload", ".yml" ]) do |file|
      file.binmode
      file.write(body)
      file.rewind
      yield Rack::Test::UploadedFile.new(file.path, "text/yaml", original_filename: filename)
    end
  end

  def sample_file
    fixture_file_upload("start_page.yml", "text/yaml")
  end

  # --- access ---

  test "requires authentication for the page" do
    get settings_import_export_url
    assert_redirected_to new_session_url
  end

  test "requires authentication to export" do
    get settings_export_url
    assert_redirected_to new_session_url
  end

  test "requires authentication to import" do
    post settings_import_export_url, params: { file: sample_file }
    assert_redirected_to new_session_url
    assert_empty @user.start_page_groups.reload
  end

  # --- the page ---

  test "shows the export link and the import form" do
    login_as @user
    get settings_import_export_url

    assert_response :success
    # One control, and it says what it exports — there is no "Export" heading
    # above it repeating the word.
    assert_select "a[href=?]", settings_export_path, text: "Export all groups and links"
    assert_select "h3", text: "Export", count: 0
    assert_select "form[action=?][enctype=?]", settings_import_export_path, "multipart/form-data" do
      assert_select "input[type=file][name=file]"
    end
  end

  # No visible label on the file field, so the accessible name has to come from
  # somewhere: aria-label is it.
  test "the file field still has an accessible name" do
    login_as @user
    get settings_import_export_url

    assert_select "input[type=file][aria-label=?]", "Start page file to import"
  end

  test "warns on the page that importing replaces everything" do
    login_as @user
    get settings_import_export_url

    assert_select "[data-turbo-confirm]"
    assert_select "#import-export", /replace/i
  end

  test "appears in the settings nav" do
    login_as @user
    get settings_url

    assert_select ".secondary-nav a[href=?]", settings_import_export_path, text: "Import & Export"
  end

  # The layouts used to build the title with Array#join, which returns a plain
  # String and drops the html_safe flag off content_for's already-escaped value —
  # so <%= %> escaped it a second time and the tab read "Import &amp; Export".
  # This is the only page whose title contains a character that shows it.
  test "the title is escaped once, not twice" do
    login_as @user
    get settings_import_export_url

    assert_select "title", text: "Import & Export - TinyStart"
    assert_no_match(/&amp;amp;/, response.body)
  end

  test "marks itself as the active tab" do
    login_as @user
    get settings_import_export_url

    assert_select ".secondary-nav a.active", text: "Import & Export"
  end

  # --- export ---

  test "exports the page as a yaml attachment" do
    build_page
    login_as @user
    get settings_export_url

    assert_response :success
    assert_equal "text/yaml", response.media_type
    assert_match(/attachment/, response.headers["Content-Disposition"])
    assert_match(/filename="tinystart-start-page-#{Date.current.iso8601}\.yml"/,
                 response.headers["Content-Disposition"])
    assert_match(/Lo de siempre/, response.body)
    assert_match(%r{https://app\.fastmail\.com}, response.body)
  end

  test "exports only your own page" do
    build_page
    build_page(@other).start_page_items.create!(title: "Theirs", url: "https://theirs.example")
    login_as @user
    get settings_export_url

    assert_not_includes response.body, "theirs.example"
  end

  test "exports an empty page without failing" do
    login_as @user
    get settings_export_url

    assert_response :success
  end

  # --- import ---

  test "imports an uploaded file" do
    login_as @user
    post settings_import_export_url, params: { file: sample_file }

    assert_redirected_to settings_import_export_path
    assert_match(/6 links/, flash[:notice])
    assert_equal 3, @user.start_page_groups.reload.count
    assert_equal 2, @user.reload.columns
  end

  test "imports for the logged-in user and no one else" do
    build_page(@other)
    login_as @user
    post settings_import_export_url, params: { file: sample_file }

    assert_equal [ "Lo de siempre" ], @other.start_page_groups.reload.map(&:name)
  end

  test "rejects a request with no file" do
    build_page
    login_as @user
    post settings_import_export_url

    assert_redirected_to settings_import_export_path
    assert_match(/choose a file/i, flash[:alert])
    assert_equal 1, @user.start_page_groups.reload.count
  end

  test "reports what was wrong with a bad file and changes nothing" do
    build_page
    login_as @user

    upload("1:\n- name: Broken\n  items:\n    Bare: example.com\n") do |file|
      post settings_import_export_url, params: { file: file }
    end

    assert_redirected_to settings_import_export_path
    assert_match(/example\.com/, flash[:alert])
    assert_equal [ "Lo de siempre" ], @user.start_page_groups.reload.map(&:name)
  end

  test "refuses a file that is too large to be a start page" do
    build_page
    login_as @user

    upload("# padding\n" * 100_000) do |file|
      post settings_import_export_url, params: { file: file }
    end

    assert_match(/too (large|big)/i, flash[:alert])
    assert_equal 1, @user.start_page_groups.reload.count
  end

  # The real data is in Spanish, so a file that is not UTF-8 would import
  # mangled names rather than fail — worth catching at the door.
  test "refuses a file that is not valid UTF-8" do
    build_page
    login_as @user

    upload("1:\n- name: \xC3\x28Bad\n  items: {}\n".b) do |file|
      post settings_import_export_url, params: { file: file }
    end

    assert_match(/UTF-8/, flash[:alert])
    assert_equal 1, @user.start_page_groups.reload.count
  end

  test "keeps accents from an uploaded file" do
    login_as @user

    upload("1:\n- name: Diseño\n  items:\n    Tipografía: https://a.example\n") do |file|
      post settings_import_export_url, params: { file: file }
    end

    assert_equal "Diseño", @user.start_page_groups.reload.first.name
    assert_equal "Tipografía", @user.start_page_items.first.title
  end

  # The loop the format is built around: export, edit, import back.
  test "a file it exported imports again unchanged" do
    build_page
    login_as @user
    get settings_export_url
    exported = response.body

    upload(exported) do |file|
      post settings_import_export_url, params: { file: file }
    end

    assert_match(/1 link/, flash[:notice])
    assert_equal [ "Lo de siempre" ], @user.start_page_groups.reload.map(&:name)
    assert_equal [ "Fastmail" ], @user.start_page_items.map(&:title)
  end
end
