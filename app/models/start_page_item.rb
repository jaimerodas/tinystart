class StartPageItem < ApplicationRecord
  belongs_to :start_page_group

  validates :url, presence: true, uniqueness: { scope: :start_page_group_id }
  validates :title, presence: true
  validates :position, presence: true,
            numericality: { only_integer: true, greater_than_or_equal_to: 0 }

  validate :valid_url

  # The add-link form sits at the bottom of a group, so it says nothing about
  # position — the group alone decides where the tile lands.
  before_validation :place_at_end_of_group, on: :create

  scope :ordered, -> { order(:position) }

  # What the tile is actually called. A rejected edit is still sitting in
  # `title` so the form can show what was typed, but the row behind it
  # describes saved state. The mirror of StartPageGroup#saved_name.
  def saved_title
    title_in_database || title
  end

  # Places this tile at a position in a group, renumbering the tiles it lands
  # between and closing the gap it leaves behind. Nil position appends.
  #
  # Same reason as StartPageGroup#move_to_column: writing an absolute position
  # without shifting anyone leaves two tiles sharing it, and `ordered` breaks
  # that tie arbitrarily. Invisible while the only caller appended to the end.
  def move_to_group(new_group, new_position = nil)
    return false unless new_group.is_a?(StartPageGroup)

    source_group = start_page_group
    changing_group = source_group != new_group
    moved = false

    transaction do
      # Validated rather than written straight through: the target group may
      # already hold this url.
      raise ActiveRecord::Rollback if changing_group && !update(start_page_group: new_group)

      neighbours = new_group.start_page_items.where.not(id: id).order(:position).to_a
      index = new_position ? new_position.clamp(0, neighbours.length) : neighbours.length
      neighbours.insert(index, self)
      neighbours.each_with_index do |item, i|
        item.update_column(:position, i) if item.position != i
      end

      source_group.reorder_positions! if changing_group
      moved = true
    end

    moved
  end

  def move_to_position(new_position)
    return false if new_position < 0

    update(position: new_position)
  end

  def reorder_group_positions!
    start_page_group.reorder_positions!
  end

  private

  def place_at_end_of_group
    return if position.present? || start_page_group.blank?

    # Positions can carry a gap when a tile is dragged away mid-list, so this
    # asks where the last one is rather than counting them.
    last = start_page_group.start_page_items.maximum(:position)
    self.position = last ? last + 1 : 0
  end

  def valid_url
    return if url.blank?

    uri = URI.parse(url)
    unless uri.is_a?(URI::HTTP) || uri.is_a?(URI::HTTPS)
      errors.add(:url, "must be a valid URL")
    end
  rescue URI::InvalidURIError
    errors.add(:url, "must be a valid URL")
  end
end
