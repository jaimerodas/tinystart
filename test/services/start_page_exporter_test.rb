require "test_helper"

class StartPageExporterTest < ActiveSupport::TestCase
  setup do
    @user = users(:one)
    @user.start_page_groups.destroy_all
  end

  def group(name, column, user: @user)
    user.start_page_groups.create!(name: name, column: column)
  end

  def tile(group, title, url)
    group.start_page_items.create!(title: title, url: url)
  end

  def export
    StartPageExporter.new(@user).call
  end

  # The header is comments; everything a parser cares about is below the ---.
  def parsed
    YAML.safe_load(export)
  end

  def header
    export.lines.take_while { |line| line.start_with?("#") }
  end

  # --- the shape of the file ---

  # The whole format in one assertion: column number => ordered groups, each a
  # name and an ordered title => url mapping. Mirrors the table in
  # docs/start-page-format.md.
  test "exports the documented structure" do
    left = group("Test 2", 1)
    tile(left, "NaN Fonts", "https://nanfonts.com")
    tile(left, "Feedbin", "https://feedbin.com")

    siempre = group("Lo de siempre", 2)
    tile(siempre, "My Synology Admin", "https://synology.local")
    tile(siempre, "Fastmail", "https://app.fastmail.com")

    otras = group("Otras cosas", 2)
    tile(otras, "YouTube", "https://youtube.com")
    tile(otras, "LinkedIn", "https://linkedin.com")

    assert_equal({
      1 => [
        { "name" => "Test 2",
          "items" => { "NaN Fonts" => "https://nanfonts.com", "Feedbin" => "https://feedbin.com" } }
      ],
      2 => [
        { "name" => "Lo de siempre",
          "items" => { "My Synology Admin" => "https://synology.local", "Fastmail" => "https://app.fastmail.com" } },
        { "name" => "Otras cosas",
          "items" => { "YouTube" => "https://youtube.com", "LinkedIn" => "https://linkedin.com" } }
      ]
    }, parsed)
  end

  test "groups appear in position order, not creation order" do
    second = group("Second", 1)
    first = group("First", 1)
    first.move_to_column(1, 0)

    assert_equal [ "First", "Second" ], parsed[1].map { |g| g["name"] }
    assert_equal 0, first.reload.position
    assert_equal 1, second.reload.position
  end

  test "tiles appear in position order, not creation order" do
    only = group("Only", 1)
    b = tile(only, "B", "https://b.example")
    a = tile(only, "A", "https://a.example")
    a.move_to_group(only, 0)

    assert_equal [ "A", "B" ], parsed[1].first["items"].keys
    assert_equal 0, a.reload.position
    assert_equal 1, b.reload.position
  end

  # --- columns are literal ---

  # An empty column is omitted, and the keys around it keep their real numbers.
  # Re-indexing here would shift the whole page left on the way back in.
  test "keeps the real column numbers when a column is empty" do
    @user.update!(columns: 3)
    tile(group("Left", 1), "L", "https://l.example")
    tile(group("Right", 3), "R", "https://r.example")

    assert_equal [ 1, 3 ], parsed.keys
    assert_equal "Right", parsed[3].first["name"]
  end

  # --- titles ---

  # tinystart's unique index is on (group, url), so one group may hold two tiles
  # with the same title — and a YAML mapping cannot. Psych would keep the last
  # and the tile would vanish from the file, so the exporter numbers them.
  test "numbers repeated titles within a group so both tiles survive" do
    only = group("Only", 1)
    tile(only, "Fastmail", "https://app.fastmail.com")
    tile(only, "Fastmail", "https://www.fastmail.com")

    assert_equal({ "Fastmail" => "https://app.fastmail.com",
                   "Fastmail (2)" => "https://www.fastmail.com" },
                 parsed[1].first["items"])
  end

  test "numbers a third repeat too" do
    only = group("Only", 1)
    3.times { |i| tile(only, "Fastmail", "https://#{i}.fastmail.com") }

    assert_equal [ "Fastmail", "Fastmail (2)", "Fastmail (3)" ],
                 parsed[1].first["items"].keys
  end

  # The suffix goes on the whole title, so a tile genuinely called "Fastmail (2)"
  # that collides becomes "Fastmail (2) (2)".
  test "suffixes the whole title" do
    only = group("Only", 1)
    tile(only, "Fastmail (2)", "https://a.fastmail.com")
    tile(only, "Fastmail (2)", "https://b.fastmail.com")

    assert_equal [ "Fastmail (2)", "Fastmail (2) (2)" ], parsed[1].first["items"].keys
  end

  test "the same title in two different groups is left alone" do
    tile(group("One", 1), "Fastmail", "https://app.fastmail.com")
    tile(group("Two", 1), "Fastmail", "https://app.fastmail.com")

    assert_equal [ "Fastmail" ], parsed[1].first["items"].keys
    assert_equal [ "Fastmail" ], parsed[1].second["items"].keys
  end

  # A warning line is built by interpolating a title into "# …", so a title
  # holding a newline used to spill onto a second line above the --- marker,
  # where it is no longer a comment and no longer parseable.
  test "a newline in a renamed title cannot break the header" do
    only = group("Only", 1)
    tile(only, "Two\nlines", "https://a.example")
    tile(only, "Two\nlines", "https://b.example")

    assert(header.all? { |line| line.start_with?("#") },
           "every header line must be a comment, got: #{header.inspect}")
    assert_equal 2, parsed[1].first["items"].length
  end

  test "warns in the header about every rename" do
    only = group("Lo de siempre", 1)
    tile(only, "Fastmail", "https://app.fastmail.com")
    tile(only, "Fastmail", "https://www.fastmail.com")

    assert_includes header,
                    %(# Renamed "Fastmail" to "Fastmail (2)" in "Lo de siempre" so both tiles survive.\n)
  end

  # --- the header ---

  test "counts the file's columns, groups and tiles" do
    tile(group("One", 1), "A", "https://a.example")
    two = group("Two", 2)
    tile(two, "B", "https://b.example")
    tile(two, "C", "https://c.example")

    assert_includes header, "# 2 columns, 2 groups, 3 tiles\n"
  end

  test "names itself and points at the format" do
    tile(group("One", 1), "A", "https://a.example")

    assert_equal "# tinystart start page export - #{Date.current.iso8601}\n", header.first
    assert_includes header, "# format: see docs/start-page-format.md\n"
  end

  # The count line describes the file, not the page: an empty trailing column is
  # not in the file, so re-importing narrows the page. Say so rather than let it
  # be discovered.
  test "warns when the page is wider than the file" do
    @user.update!(columns: 3)
    tile(group("One", 1), "A", "https://a.example")

    assert_includes header, "# 1 column, 1 group, 1 tile\n"
    assert_includes header,
                    "# The page is 3 columns wide but nothing is past column 1, " \
                    "so importing this file will set it to 1.\n"
  end

  test "says nothing about width when the page is full" do
    @user.update!(columns: 1)
    tile(group("One", 1), "A", "https://a.example")

    assert_not(header.any? { |line| line.include?("columns wide") })
  end

  # --- edges ---

  test "exports an empty page without raising" do
    assert_equal({}, parsed)
    assert_includes header, "# 0 columns, 0 groups, 0 tiles\n"
  end

  test "exports a group with no tiles" do
    group("Empty", 1)

    assert_equal({}, parsed[1].first["items"])
  end

  test "exports only the given user's page" do
    tile(group("Mine", 1), "Mine", "https://mine.example")
    tile(group("Theirs", 1, user: users(:two)), "Theirs", "https://theirs.example")

    assert_equal [ "Mine" ], parsed[1].map { |g| g["name"] }
    assert_not_includes export, "theirs.example"
  end

  # UTF-8 in, UTF-8 out — the real data is in Spanish.
  test "keeps accented names readable" do
    tile(group("Diseño", 1), "Tipografía", "https://a.example")

    assert_equal "Diseño", parsed[1].first["name"]
    assert_includes export, "Diseño"
  end
end
