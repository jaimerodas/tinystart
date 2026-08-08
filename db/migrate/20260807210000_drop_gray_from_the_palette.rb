class DropGrayFromThePalette < ActiveRecord::Migration[8.1]
  # Gray left the palette. Rows still holding it would fail validation on the
  # next save, and would render with no matching [data-color] rule, so they
  # move to the default accent instead.
  def up
    execute "UPDATE users SET color_preference = 'teal' WHERE color_preference = 'gray'"
  end

  def down
    # Nothing to restore: which users were gray is not recorded anywhere.
    raise ActiveRecord::IrreversibleMigration
  end
end
