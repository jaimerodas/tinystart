class CreateStartPageGroups < ActiveRecord::Migration[8.1]
  def change
    create_table :start_page_groups do |t|
      t.references :start_page, null: false, foreign_key: true
      t.string :name, null: false
      t.integer :column, null: false
      t.integer :position, null: false
      t.timestamps
    end

    add_index :start_page_groups, [ :start_page_id, :column, :position ]
    add_index :start_page_groups, [ :start_page_id, :name ], unique: true
  end
end
