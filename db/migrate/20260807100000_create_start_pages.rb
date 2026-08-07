class CreateStartPages < ActiveRecord::Migration[8.1]
  def change
    create_table :start_pages do |t|
      t.references :user, null: false, foreign_key: true, index: { unique: true }
      t.string :name, null: false
      t.integer :columns, null: false, default: 3
      t.timestamps
    end
  end
end
