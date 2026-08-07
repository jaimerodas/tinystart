class StartPageGroup < ApplicationRecord
  belongs_to :start_page
  has_many :start_page_items, dependent: :destroy

  validates :name, presence: true, uniqueness: { scope: :start_page_id }
  validates :column, presence: true,
            numericality: { only_integer: true, greater_than: 0 }
  validates :position, presence: true,
            numericality: { only_integer: true, greater_than_or_equal_to: 0 }

  validate :column_within_start_page_limit

  scope :in_column, ->(col) { where(column: col) }
  scope :ordered, -> { order(:column, :position) }

  def ordered_items
    start_page_items.order(:position)
  end

  def move_to_column(new_column, new_position = nil)
    return false if new_column > start_page.columns

    self.column = new_column
    self.position = new_position || next_position_in_column(new_column)
    save
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

  def next_position_in_column(col)
    max_position = start_page.start_page_groups.in_column(col).maximum(:position)
    max_position ? max_position + 1 : 0
  end

  def column_within_start_page_limit
    if column && start_page && column > start_page.columns
      errors.add(:column, "cannot exceed start page column limit of #{start_page.columns}")
    end
  end
end
