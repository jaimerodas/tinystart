# Helper for proving that position reordering is atomic.
module PositionWriteFailure
  # Runs the real update_column until it is asked to write `position`, then
  # blows up. A stub cannot do this: it would suppress the earlier writes too,
  # leaving nothing for the rollback to undo.
  def failing_position_write(position)
    original = StartPageItem.instance_method(:update_column)
    StartPageItem.define_method(:update_column) do |name, value|
      raise ActiveRecord::StatementInvalid, "database is locked" if value == position
      original.bind(self).call(name, value)
    end

    yield
  ensure
    StartPageItem.remove_method(:update_column)
  end
end
