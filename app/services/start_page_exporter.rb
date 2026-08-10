# Writes a user's start page as the interchange format in
# docs/start-page-format.md: a mapping of column number → ordered list of
# groups, each group a name and an ordered title → url mapping.
#
# Two things here are not obvious from the format doc, because tinystart never
# produced the format until now:
#
# - Titles are deduped. The unique index is on (group, url), so one group may
#   hold two tiles called the same thing — and a YAML mapping cannot. Psych
#   keeps the last of two identical keys and says nothing, so an undeduped
#   export would silently drop a tile.
# - An empty trailing column is not in the file, so re-importing narrows the
#   page. The header says so rather than letting it be discovered.
class StartPageExporter
  def initialize(user)
    @user = user
    @renames = []
  end

  # => String: comment header, then the YAML document.
  def call
    data = build
    header(data) + data.to_yaml
  end

  private

  def build
    groups = @user.start_page_groups.ordered.includes(:start_page_items)

    groups.each_with_object({}) do |group, data|
      (data[group.column] ||= []) << { "name" => group.name, "items" => items_for(group) }
    end
  end

  # Sorted in Ruby rather than through #ordered_items: that would be a query per
  # group, and the preload above already has the tiles in memory.
  def items_for(group)
    group.start_page_items.sort_by(&:position).each_with_object({}) do |item, items|
      items[unique_title(item.title, items, group)] = item.url
    end
  end

  # "Fastmail", then "Fastmail (2)", then "Fastmail (3)". The suffix goes on the
  # whole title, so a tile genuinely called "Fastmail (2)" that collides becomes
  # "Fastmail (2) (2)" — which is what tinylinks' exporter does, and what the
  # format doc tells a reader to expect.
  def unique_title(title, taken, group)
    return title unless taken.key?(title)

    suffix = 2
    suffix += 1 while taken.key?("#{title} (#{suffix})")
    numbered = "#{title} (#{suffix})"

    @renames << %(Renamed "#{title}" to "#{numbered}" in "#{group.name}" so both tiles survive.)
    numbered
  end

  def header(data)
    columns = data.keys.max || 0
    groups = data.values.sum(&:length)
    tiles = data.values.flatten.sum { |group| group["items"].length }

    lines = [
      "tinystart start page export - #{Date.current.iso8601}",
      "#{count(columns, 'column')}, #{count(groups, 'group')}, #{count(tiles, 'tile')}",
      "format: see docs/start-page-format.md"
    ]
    lines << width_warning(columns) if columns.positive? && @user.columns > columns
    lines.concat(@renames)

    # Squished, not just prefixed: a title or group name is only presence-
    # validated, so one holding a newline would spill a warning onto a second
    # line above the --- marker, where it is no longer a comment and the file no
    # longer parses.
    lines.map { |line| "# #{line.gsub(/\s+/, ' ')}\n" }.join
  end

  # The counts describe the file, not the page — that is what a re-import
  # reproduces, and what makes them worth checking on the way back in.
  def count(number, noun)
    "#{number} #{noun.pluralize(number)}"
  end

  def width_warning(columns)
    "The page is #{@user.columns} columns wide but nothing is past column " \
    "#{columns}, so importing this file will set it to #{columns}."
  end
end
