require "test_helper"

class StartPageImporterTest < ActiveSupport::TestCase
  setup do
    @user = users(:one)
    @user.start_page_groups.destroy_all
  end

  # The file from docs/start-page-format.md, header and all.
  SAMPLE = <<~YAML
    # tinylinks start page export - 2026-08-10
    # 2 columns, 3 groups, 6 tiles
    # format: see docs/start-page-format.md in tinystart
    ---
    1:
    - name: Test 2
      items:
        NaN Fonts: https://nanfonts.com
        Feedbin: https://feedbin.com
    2:
    - name: Lo de siempre
      items:
        My Synology Admin: https://synology.local
        Fastmail: https://app.fastmail.com
    - name: Otras cosas
      items:
        YouTube: https://youtube.com
        LinkedIn: https://linkedin.com
  YAML

  def import(source)
    @importer = StartPageImporter.new(@user, source)
    @importer.call
  end

  def error
    @importer.error
  end

  # Builds a file with a correct header for the body given, so a test about
  # anything other than the header does not have to count for itself.
  def file(body, groups: nil, tiles: nil, columns: nil)
    data = YAML.safe_load(body) || {}
    columns ||= data.keys.grep(Integer).max || 0
    all_groups = data.values.select { |v| v.is_a?(Array) }.flatten
    groups ||= all_groups.length
    tiles ||= all_groups.sum { |g| g.is_a?(Hash) && g["items"].is_a?(Hash) ? g["items"].length : 0 }

    "# test file\n" \
    "# #{columns} #{'column'.pluralize(columns)}, #{groups} #{'group'.pluralize(groups)}, " \
    "#{tiles} #{'tile'.pluralize(tiles)}\n---\n#{body}"
  end

  def page
    @user.start_page_groups.reload.ordered.map do |group|
      [ group.column, group.position, group.name, group.ordered_items.map(&:title) ]
    end
  end

  # --- the happy path ---

  # The table in docs/start-page-format.md, asserted.
  test "builds the documented page from the documented file" do
    assert import(SAMPLE), error

    assert_equal 2, @user.reload.columns
    assert_equal [
      [ 1, 0, "Test 2", [ "NaN Fonts", "Feedbin" ] ],
      [ 2, 0, "Lo de siempre", [ "My Synology Admin", "Fastmail" ] ],
      [ 2, 1, "Otras cosas", [ "YouTube", "LinkedIn" ] ]
    ], page
  end

  test "gives tiles the file's order as their position" do
    assert import(SAMPLE), error

    tiles = @user.start_page_groups.find_by!(name: "Test 2").ordered_items
    assert_equal [ 0, 1 ], tiles.map(&:position)
    assert_equal [ "https://nanfonts.com", "https://feedbin.com" ], tiles.map(&:url)
  end

  test "leaves visit counts at zero" do
    assert import(SAMPLE), error

    assert_equal [ 0 ], @user.start_page_items.pluck(:visit_count).uniq
  end

  test "reports what it imported" do
    assert import(SAMPLE), error

    assert_equal({ columns: 2, groups: 3, items: 6 }, @importer.summary)
  end

  test "keeps accented names and titles intact" do
    assert import(file(<<~YAML)), error
      1:
      - name: Diseño
        items:
          Tipografía: https://a.example
    YAML

    assert_equal "Diseño", @user.start_page_groups.first.name
    assert_equal "Tipografía", @user.start_page_items.first.title
  end

  # --- it replaces ---

  test "replaces the page that was there" do
    old = @user.start_page_groups.create!(name: "Old", column: 1)
    old.start_page_items.create!(title: "Gone", url: "https://gone.example")

    assert import(SAMPLE), error

    assert_not StartPageGroup.exists?(old.id)
    assert_not_includes @user.start_page_items.reload.map(&:url), "https://gone.example"
    assert_equal 3, @user.start_page_groups.reload.count
  end

  test "importing the same file twice is the same page" do
    assert import(SAMPLE), error
    first = page

    assert import(SAMPLE), error
    assert_equal first, page
  end

  test "leaves another user's page alone" do
    other = users(:two)
    other.start_page_groups.create!(name: "Theirs", column: 1)
       .start_page_items.create!(title: "Theirs", url: "https://theirs.example")

    assert import(SAMPLE), error

    assert_equal [ "Theirs" ], other.start_page_groups.reload.map(&:name)
  end

  # --- columns ---

  # The single most likely way to get this wrong: iterating the values would put
  # "Right" in column 2 and shift the page left.
  test "reads the column from the key and never re-indexes" do
    assert import(file(<<~YAML)), error
      1:
      - name: Left
        items:
          L: https://l.example
      3:
      - name: Right
        items:
          R: https://r.example
    YAML

    assert_equal 3, @user.reload.columns
    assert_equal 3, @user.start_page_groups.find_by!(name: "Right").column
  end

  # columns defaults to 1 and column_within_user_limit rejects anything past it,
  # so widening has to happen before the first group is created. This test fails
  # if the two are reordered.
  test "widens the page before creating groups" do
    @user.update!(columns: 1)

    assert import(file(<<~YAML)), error
      3:
      - name: Far right
        items:
          R: https://r.example
    YAML

    assert_equal 3, @user.reload.columns
  end

  test "narrows the page when the file is narrower" do
    @user.update!(columns: 3)

    assert import(file(<<~YAML)), error
      1:
      - name: Only
        items:
          A: https://a.example
    YAML

    assert_equal 1, @user.reload.columns
  end

  test "refuses a column past the six the page allows" do
    assert_not import(file(<<~YAML))
      7:
      - name: Too far
        items:
          A: https://a.example
    YAML

    assert_match(/7/, error)
    assert_empty page
  end

  test "refuses a column of zero" do
    assert_not import(file(<<~YAML))
      0:
      - name: Nowhere
        items:
          A: https://a.example
    YAML

    assert_empty page
  end

  # --- nothing is written on failure ---

  # The page as it was, so every refusal below can assert it is still there.
  def existing_page
    group = @user.start_page_groups.create!(name: "Keep", column: 1)
    group.start_page_items.create!(title: "Keep me", url: "https://keep.example")
    @user.update!(columns: 2)
    page
  end

  test "an invalid url writes nothing" do
    before = existing_page

    assert_not import(file(<<~YAML))
      1:
      - name: Fine
        items:
          Bare: example.com
    YAML

    assert_match(/example\.com/, error)
    assert_match(/valid URL/i, error)
    assert_equal before, page
    assert_equal 2, @user.reload.columns
  end

  test "a repeated group name writes nothing" do
    before = existing_page

    assert_not import(file(<<~YAML))
      1:
      - name: Twice
        items:
          A: https://a.example
      - name: Twice
        items:
          B: https://b.example
    YAML

    assert_match(/Twice/, error)
    assert_equal before, page
  end

  test "a repeated url within one group writes nothing" do
    before = existing_page

    assert_not import(file(<<~YAML))
      1:
      - name: Group
        items:
          One: https://same.example
          Two: https://same.example
    YAML

    assert_match(/same\.example/, error)
    assert_equal before, page
  end

  # The transaction protects the database from any write failure; this protects
  # the caller's contract, which is that a failed import reports why rather than
  # raising past the controller and losing the page's message.
  test "a database failure is reported, not raised" do
    before = existing_page
    StartPageItem.any_instance.stubs(:save!).raises(
      ActiveRecord::StatementInvalid.new("SQLITE_BUSY")
    )

    assert_not import(SAMPLE)

    assert_not_nil error
    assert_equal before, page
    assert_equal 2, @user.reload.columns
  end

  test "a missing title writes nothing" do
    before = existing_page

    assert_not import(file(<<~YAML))
      1:
      - name: Group
        items:
          "": https://a.example
    YAML

    assert_equal before, page
  end

  # --- the header count check ---

  # Psych keeps the last of two identical keys and says nothing, so the tile is
  # gone with no error from anywhere. The header counts are the only way to see
  # it happened — hence a warning on the way through, not silence.
  test "warns when a duplicate title collapsed and the tile count fell short" do
    assert import(<<~YAML), error
      # hand-edited
      # 1 column, 1 group, 2 tiles
      ---
      1:
      - name: Group
        items:
          Same: https://a.example
          Same: https://b.example
    YAML

    # The tile really is gone — that is the point of noticing.
    assert_equal [ [ 1, 0, "Group", [ "Same" ] ] ], page
    assert_match(/2 tiles/, @importer.warning)
    assert_match(/1 tile/, @importer.warning)
  end

  # It cannot be a refusal. Deleting a tile by hand lowers the count in exactly
  # the way a collapsed duplicate key does, and the file cannot say which
  # happened — so refusing would block the one workflow this format is for.
  test "a hand edit that removes a tile imports, and is not called corruption" do
    assert import(<<~YAML), error
      # tinystart start page export - 2026-08-10
      # 1 column, 1 group, 2 tiles
      ---
      1:
      - name: Group
        items:
          Kept: https://a.example
    YAML

    assert_equal [ [ 1, 0, "Group", [ "Kept" ] ] ], page
    assert_match(/edited/i, @importer.warning)
  end

  test "warns when the group count does not match the header" do
    assert import(<<~YAML), error
      # 1 column, 2 groups, 1 tile
      ---
      1:
      - name: Group
        items:
          A: https://a.example
    YAML

    assert_match(/groups/, @importer.warning)
    assert_equal 1, @user.start_page_groups.reload.count
  end

  test "warns when the column count does not match the header" do
    assert import(<<~YAML), error
      # 2 columns, 1 group, 1 tile
      ---
      1:
      - name: Group
        items:
          A: https://a.example
    YAML

    assert_match(/columns/, @importer.warning)
    assert_equal 1, @user.reload.columns
  end

  test "says nothing when the counts agree" do
    assert import(SAMPLE), error

    assert_nil @importer.warning
  end

  # The counts line is the only thing that sees a collapsed duplicate key, so a
  # byte order mark above it must not switch the check off.
  test "still checks the counts when the file starts with a byte order mark" do
    assert import(+"﻿" << <<~YAML), error
      # 1 column, 1 group, 2 tiles
      ---
      1:
      - name: Group
        items:
          Same: https://a.example
          Same: https://b.example
    YAML

    assert_not_nil @importer.warning
    assert_match(/2 tiles/, @importer.warning)
  end

  test "still checks the counts when a blank line sits above the header" do
    assert import(<<~YAML), error

      # 1 column, 1 group, 2 tiles
      ---
      1:
      - name: Group
        items:
          Same: https://a.example
          Same: https://b.example
    YAML

    assert_not_nil @importer.warning
  end

  # The counts are a check on a file that offers them, not a requirement.
  test "imports a file with no header at all" do
    assert import(<<~YAML), error
      ---
      1:
      - name: Group
        items:
          A: https://a.example
    YAML

    assert_equal [ [ 1, 0, "Group", [ "A" ] ] ], page
  end

  test "ignores header lines it does not recognise" do
    assert import(<<~YAML), error
      # tinylinks start page export - 2026-08-10
      # format: see docs/start-page-format.md in tinystart
      # Renamed "Fastmail" to "Fastmail (2)" in "Lo de siempre" so both tiles survive.
      ---
      1:
      - name: Group
        items:
          A: https://a.example
    YAML

    assert_equal 1, @user.start_page_groups.reload.count
  end

  # --- malformed input ---

  test "refuses YAML that does not parse" do
    before = existing_page

    assert_not import("1:\n- name: [unclosed\n")

    assert_match(/could not be read|isn't valid/i, error)
    assert_equal before, page
  end

  # safe_load with aliases off: the format permits String, Integer, Hash and
  # Array and nothing else.
  test "refuses aliases" do
    before = existing_page

    assert_not import(<<~YAML)
      1: &ref
      - name: Group
        items:
          A: https://a.example
      2: *ref
    YAML

    assert_equal before, page
  end

  test "refuses a disallowed class" do
    before = existing_page

    assert_not import(<<~YAML)
      1:
      - name: !ruby/object:Struct {}
        items: {}
    YAML

    assert_equal before, page
  end

  test "refuses an empty file rather than wiping the page" do
    before = existing_page

    assert_not import("")
    assert_equal before, page

    assert_not import("--- {}\n")
    assert_equal before, page
    assert_match(/empty|no groups/i, error)
  end

  # A column key with an empty list under it is a mapping with columns in it and
  # no groups. It passed every shape check, matched a 0-group header, and then
  # deleted the page and reported "Imported 0 links" as a success.
  test "refuses a file whose columns hold no groups" do
    before = existing_page

    assert_not import("1: []\n")
    assert_equal before, page
    assert_match(/no groups/i, error)

    assert_not import(file("1: []\n2: []\n"))
    assert_equal before, page
  end

  test "refuses a file that is not a mapping" do
    before = existing_page

    assert_not import("--- just a string\n")
    assert_equal before, page

    assert_not import("---\n- one\n- two\n")
    assert_equal before, page
  end

  # Every key in this format is an Integer, so a String key is a later format
  # with an envelope rather than a broken file — the one thing worth saying
  # differently.
  test "refuses a file with a version envelope" do
    before = existing_page

    assert_not import(<<~YAML)
      version: 2
      columns:
        1:
        - name: Group
          items: {}
    YAML

    assert_match(/newer format/i, error)
    assert_equal before, page
  end

  test "refuses a column whose value is not a list of groups" do
    before = existing_page

    assert_not import(file("1: not a list\n", groups: 0, tiles: 0))
    assert_match(/column 1/i, error)
    assert_equal before, page
  end

  test "refuses a group that is missing its name" do
    before = existing_page

    assert_not import(file(<<~YAML))
      1:
      - items:
          A: https://a.example
    YAML

    assert_match(/column 1/i, error)
    assert_equal before, page
  end

  test "refuses a group whose items are not a mapping" do
    before = existing_page

    assert_not import(file("1:\n- name: Group\n  items: nope\n", tiles: 0))
    assert_match(/Group/, error)
    assert_equal before, page
  end

  test "accepts a group with an empty items mapping" do
    assert import(file("1:\n- name: Empty\n  items: {}\n")), error

    assert_equal [ [ 1, 0, "Empty", [] ] ], page
  end

  # A hand edit can leave an unquoted key that YAML does not hand back as a
  # String. The doc says coerce rather than reject.
  test "coerces a numeric title" do
    assert import(file(<<~YAML)), error
      1:
      - name: Group
        items:
          123: https://a.example
    YAML

    assert_equal "123", @user.start_page_items.first.title
  end

  # --- the round trip ---

  # The real proof, and what makes "export, hand-edit, import" safe.
  test "survives a round trip through the exporter" do
    @user.update!(columns: 3)
    left = @user.start_page_groups.create!(name: "Diseño", column: 1)
    left.start_page_items.create!(title: "Tipografía", url: "https://nanfonts.com")
    left.start_page_items.create!(title: "Feedbin", url: "https://feedbin.com")
    right = @user.start_page_groups.create!(name: "Otras cosas", column: 3)
    right.start_page_items.create!(title: "YouTube", url: "https://youtube.com")
    before = page

    assert import(StartPageExporter.new(@user).call), error

    assert_equal before, page
    assert_equal 3, @user.reload.columns
  end

  test "a round trip renumbers a repeated title rather than losing the tile" do
    group = @user.start_page_groups.create!(name: "Only", column: 1)
    group.start_page_items.create!(title: "Fastmail", url: "https://app.fastmail.com")
    group.start_page_items.create!(title: "Fastmail", url: "https://www.fastmail.com")

    assert import(StartPageExporter.new(@user).call), error

    assert_equal [ [ 1, 0, "Only", [ "Fastmail", "Fastmail (2)" ] ] ], page
    assert_equal [ "https://app.fastmail.com", "https://www.fastmail.com" ],
                 @user.start_page_items.reload.order(:position).map(&:url)
  end

  test "an empty page round trips as a refusal, not a wipe" do
    assert_not import(StartPageExporter.new(@user).call)
    assert_empty page
  end
end
