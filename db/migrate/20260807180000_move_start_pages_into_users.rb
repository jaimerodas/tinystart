class MoveStartPagesIntoUsers < ActiveRecord::Migration[8.1]
  # A start page was never more than a column count with a name nobody read, and
  # it was already 1:1 with its user. Collapse it: columns onto users, groups
  # pointed straight at the user.
  def up
    add_column :users, :columns, :integer, default: 3, null: false
    add_reference :start_page_groups, :user, foreign_key: true

    execute <<~SQL
      UPDATE users
      SET columns = (SELECT start_pages.columns FROM start_pages WHERE start_pages.user_id = users.id)
      WHERE EXISTS (SELECT 1 FROM start_pages WHERE start_pages.user_id = users.id)
    SQL

    execute <<~SQL
      UPDATE start_page_groups
      SET user_id = (SELECT start_pages.user_id FROM start_pages WHERE start_pages.id = start_page_groups.start_page_id)
    SQL

    # An orphan here would have no user to belong to, and the grid can't render it.
    execute "DELETE FROM start_page_groups WHERE user_id IS NULL"
    change_column_null :start_page_groups, :user_id, false

    add_index :start_page_groups, [ :user_id, :column, :position ]
    add_index :start_page_groups, [ :user_id, :name ], unique: true

    # SQLite rebuilds the table on remove_reference and keeps every index that
    # still has a column left. Drop the composites first or the name index
    # survives as a globally unique index on name alone, which would stop two
    # users from each having a "Work" group.
    remove_index :start_page_groups, name: "idx_on_start_page_id_column_position_daed7dd0d0"
    remove_index :start_page_groups, name: "index_start_page_groups_on_start_page_id_and_name"
    remove_reference :start_page_groups, :start_page, foreign_key: true, null: false

    drop_table :start_pages
  end

  def down
    create_table :start_pages do |t|
      t.references :user, null: false, foreign_key: true, index: { unique: true }
      t.string :name, null: false
      t.integer :columns, null: false, default: 3
      t.timestamps
    end

    execute <<~SQL
      INSERT INTO start_pages (user_id, name, columns, created_at, updated_at)
      SELECT id, 'Start', columns, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP FROM users
    SQL

    add_reference :start_page_groups, :start_page, foreign_key: true

    execute <<~SQL
      UPDATE start_page_groups
      SET start_page_id = (SELECT start_pages.id FROM start_pages WHERE start_pages.user_id = start_page_groups.user_id)
    SQL

    change_column_null :start_page_groups, :start_page_id, false
    add_index :start_page_groups, [ :start_page_id, :column, :position ],
              name: "idx_on_start_page_id_column_position_daed7dd0d0"
    add_index :start_page_groups, [ :start_page_id, :name ], unique: true,
              name: "index_start_page_groups_on_start_page_id_and_name"

    remove_index :start_page_groups, name: "index_start_page_groups_on_user_id_and_column_and_position"
    remove_index :start_page_groups, name: "index_start_page_groups_on_user_id_and_name"
    remove_reference :start_page_groups, :user, foreign_key: true, null: false

    remove_column :users, :columns
  end
end
