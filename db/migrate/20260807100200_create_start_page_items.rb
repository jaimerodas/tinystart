class CreateStartPageItems < ActiveRecord::Migration[8.1]
  def change
    create_table :start_page_items do |t|
      # Tiles own their url and title outright — no pointer to anything else.
      t.references :start_page_group, null: false, foreign_key: true
      t.string :url, null: false
      t.string :title, null: false
      t.integer :position, null: false
      t.integer :visit_count, null: false, default: 0
      t.timestamps
    end

    add_index :start_page_items, [ :start_page_group_id, :position ]
    add_index :start_page_items, [ :start_page_group_id, :url ], unique: true
  end
end
