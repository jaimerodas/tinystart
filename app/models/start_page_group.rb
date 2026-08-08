class StartPageGroup < ApplicationRecord
  belongs_to :user
  has_many :start_page_items, dependent: :destroy

  validates :name, presence: true, uniqueness: { scope: :user_id }
  validates :column, presence: true,
            numericality: { only_integer: true, greater_than: 0 }
  validates :position, presence: true,
            numericality: { only_integer: true, greater_than_or_equal_to: 0 }

  validate :column_within_user_limit

  # The add-group form lives at the bottom of a column, so it says nothing
  # about position — the column alone decides where the group lands.
  before_validation :place_at_end_of_column, on: :create

  scope :in_column, ->(col) { where(column: col) }
  scope :ordered, -> { order(:column, :position) }

  def ordered_items
    start_page_items.order(:position)
  end

  # What the group is actually called. A rejected rename is still sitting in
  # `name` so the form can show what was typed, but the header and the labels
  # around it describe saved state, not an edit in flight.
  def saved_name
    name_in_database || name
  end

  # Places this group at a position in a column, renumbering the groups it
  # lands between and closing the gap it leaves behind. Nil position appends.
  #
  # Writing an absolute position without shifting anyone leaves two groups
  # sharing it, and `ordered` then breaks the tie arbitrarily. That stayed
  # invisible while the move buttons were the only caller — they only ever
  # trade places with a neighbour — but a drag can drop a group anywhere.
  def move_to_column(new_column, new_position = nil)
    # The renumbering below uses update_column, which skips the numericality
    # validation `save` used to enforce — so the bounds are checked here or not
    # at all. A group parked outside column_range renders nowhere and has no UI
    # left to bring it back.
    new_column = new_column.to_i
    return false unless new_column.positive? && new_column <= user.columns

    source_column = column

    transaction do
      neighbours = user.start_page_groups.in_column(new_column).where.not(id: id).order(:position).to_a
      index = new_position ? new_position.clamp(0, neighbours.length) : neighbours.length
      neighbours.insert(index, self)

      update_column(:column, new_column) if source_column != new_column
      neighbours.each_with_index do |group, i|
        group.update_column(:position, i) if group.position != i
      end

      user.reorder_groups_in_column!(source_column) if source_column != new_column
    end

    true
  end

  def move_item_to_position(item, new_position)
    # Items from another group (or already deleted) have no index here
    group_items = start_page_items.order(:position).to_a
    current_index = group_items.index(item)

    return false unless current_index

    # Determine the new index based on the new position
    new_index = group_items.find_index { |i| i.position >= new_position }
    new_index ||= group_items.length

    # Remove item from current position and insert at new position
    group_items.delete_at(current_index)
    group_items.insert(new_index, item)

    reorder_positions!(group_items)

    true
  rescue ArgumentError
    # new_position could not be compared against the existing positions
    false
  end

  # Numbers items 0, 1, 2... in the order given, closing any gaps. All of the
  # positions land or none of them do. Defaults to the group's current order.
  def reorder_positions!(items = start_page_items.order(:position))
    transaction do
      items.each_with_index do |item, index|
        item.update_column(:position, index) if item.position != index
      end
    end
  end

  private

  def place_at_end_of_column
    return if position.present? || column.blank? || user.blank?

    self.position = next_position_in_column(column)
  end

  def next_position_in_column(col)
    max_position = user.start_page_groups.in_column(col).maximum(:position)
    max_position ? max_position + 1 : 0
  end

  def column_within_user_limit
    if column && user && column > user.columns
      errors.add(:column, "cannot exceed start page column limit of #{user.columns}")
    end
  end
end
