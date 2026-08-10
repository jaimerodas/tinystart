# Rebuilds a user's start page from the interchange format in
# docs/start-page-format.md.
#
# It replaces rather than merges: the user's groups are destroyed and the page is
# built again from the file. That is what makes the workflow the format is for —
# export, look at it, hand-edit the YAML, import again — idempotent.
#
# Because it replaces, every check happens before anything is written and the
# whole write is one transaction. A file that is refused leaves the page exactly
# as it was; a file that is accepted has already been checked.
class StartPageImporter
  MAX_COLUMNS = 6

  # "2 columns, 3 groups, 6 tiles" off the comment header.
  HEADER_COUNTS = /^\s*#\s*(\d+) columns?, (\d+) groups?, (\d+) tiles?\s*$/

  # `warning` is set on a successful import that looked odd — see count_mismatch.
  attr_reader :error, :summary, :warning

  def initialize(user, source)
    @user = user
    @source = source.to_s.delete_prefix("﻿")
  end

  def call
    data = parse
    return false if @error
    return false unless valid_shape?(data)

    @warning = count_mismatch(data)
    write(data)
  end

  private

  # safe_load, never load, and aliases stay off: the format permits String,
  # Integer, Hash and Array and nothing else.
  def parse
    YAML.safe_load(@source)
  rescue Psych::Exception => e
    fail_with("that file could not be read as YAML — #{e.message.lines.first.to_s.strip}")
    nil
  end

  def valid_shape?(data)
    return empty_file if data.nil?

    unless data.is_a?(Hash)
      return fail_with("that file isn't a mapping of column numbers to groups — " \
                       "see docs/start-page-format.md")
    end

    # Every top-level key in this format is an Integer, so a String key is a
    # later format with an envelope rather than a broken file.
    unless data.keys.all?(Integer)
      return fail_with("that file looks like a newer format than this app can read: " \
                       "every top-level key should be a column number")
    end

    return false unless data.all? { |column, groups| valid_column?(column, groups) }

    # Checked on the groups rather than on the mapping, because `1: []` is a
    # mapping with a column in it and no groups anywhere — a legal instruction to
    # delete the page, which is never what picking a file meant.
    return empty_file if group_list(data).empty?

    true
  end

  def valid_column?(column, groups)
    unless column.between?(1, MAX_COLUMNS)
      return fail_with("column #{column} is outside the 1–#{MAX_COLUMNS} columns " \
                       "a start page can have")
    end

    unless groups.is_a?(Array)
      return fail_with("column #{column} should be a list of groups, but it isn't")
    end

    groups.each_with_index.all? { |group, index| valid_group?(group, column, index) }
  end

  def valid_group?(group, column, index)
    unless group.is_a?(Hash) && group["name"].present?
      return fail_with("the group at position #{index + 1} of column #{column} has no name")
    end

    unless group["items"].is_a?(Hash)
      return fail_with(%(the group "#{group["name"]}" has no items mapping — ) +
                       "a group with no tiles is written as `items: {}`")
    end

    true
  end

  # Psych keeps the last of two identical keys and says nothing, so a hand edit
  # that repeats a title inside one `items` block loses a tile with no error from
  # anywhere. The header's counts are the only cheap way to see that happened.
  #
  # It cannot be a refusal, though, and that is the whole subtlety: deleting a
  # tile by hand lowers the count in exactly the way a collapsed key does, and
  # nothing in the file says which happened. Refusing would block the one
  # workflow this format exists for — export, edit, import again — so the import
  # goes through and reports what it noticed. A file with no counts line says
  # nothing at all.
  #
  # Note it cannot see a repeated *group* name: groups are list items, not
  # mapping keys, so duplicating one changes no count. That fails later, on
  # StartPageGroup's uniqueness validation, which names it properly.
  def count_mismatch(data)
    stated = header_counts
    return nil unless stated

    actual = [ data.keys.max, group_list(data).length, tile_count(data) ]
    return nil if stated == actual

    "Its header describes #{describe_counts(stated)}, but #{describe_counts(actual)} " \
    "came in — expected if you edited the file, worth a look if you didn't."
  end

  def describe_counts(counts)
    columns, groups, tiles = counts
    "#{columns} #{'column'.pluralize(columns)}, #{groups} #{'group'.pluralize(groups)} " \
    "and #{tiles} #{'tile'.pluralize(tiles)}"
  end

  # Every line above the document marker, rather than the leading run of
  # comments: a byte order mark or a blank line above the header ends that run on
  # its first line and would skip the check entirely — and this check is the only
  # thing that ever sees a collapsed duplicate key.
  def header_counts
    match = @source.lines
      .take_while { |line| line.strip != "---" }
      .lazy.filter_map { |line| line.match(HEADER_COUNTS) }.first
    match&.captures&.map(&:to_i)
  end

  def group_list(data)
    data.values.flatten
  end

  def tile_count(data)
    group_list(data).sum { |group| group["items"].length }
  end

  def write(data)
    columns = data.keys.max
    groups = 0
    items = 0

    @user.transaction do
      @user.start_page_groups.destroy_all
      @user.start_page_groups.reset

      # Set the width before the first group: users.columns defaults to 1 and
      # StartPageGroup#column_within_user_limit rejects any group past it, so a
      # file using column 3 fails on its first group otherwise. Narrowing is
      # safe here for the mirror-image reason — the groups
      # User#columns_leave_no_group_stranded would refuse to hide are already
      # gone.
      @user.update!(columns: columns)

      data.keys.sort.each do |column|
        data[column].each do |attrs|
          group = @user.start_page_groups.create!(name: attrs["name"], column: column)
          groups += 1

          # No position on either create: place_at_end_of_column and
          # place_at_end_of_group fill it in, so creating in file order
          # reproduces the file's order with no arithmetic.
          attrs["items"].each do |title, url|
            group.start_page_items.create!(title: title.to_s, url: url.to_s)
            items += 1
          end
        end
      end
    end

    @summary = { columns: columns, groups: groups, items: items }
    true
  rescue ActiveRecord::RecordInvalid => e
    rolled_back
    fail_with(describe(e.record))
  rescue ActiveRecord::ActiveRecordError => e
    # The transaction has protected the database either way. This is about the
    # caller's contract: a failed import says why it failed, rather than a
    # database-level error becoming a 500 with no message on the page.
    rolled_back
    fail_with("the database refused the import (#{e.class})")
  end

  # The transaction has already rolled the database back; this drops the
  # in-memory copy of what it rolled back, `columns` above all.
  def rolled_back
    @user.reload
  end

  def describe(record)
    messages = record.errors.full_messages.to_sentence

    case record
    when StartPageItem
      %(the link "#{record.title}" (#{record.url}) in ) +
        %("#{record.start_page_group.name}" was rejected: #{messages})
    when StartPageGroup
      %(the group "#{record.name}" was rejected: #{messages})
    else
      messages
    end
  end

  def empty_file
    fail_with("that file has no groups in it — importing it would only empty your " \
              "start page, so nothing was changed")
  end

  def fail_with(message)
    @error = message
    false
  end
end
