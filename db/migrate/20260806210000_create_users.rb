class CreateUsers < ActiveRecord::Migration[8.1]
  def change
    create_table :users do |t|
      t.string :email, null: false
      t.string :password_digest, null: false

      # New signups wait for an admin; the first user ever created is
      # bootstrapped as an approved admin.
      t.boolean :approved, null: false, default: false
      t.boolean :admin, null: false, default: false

      t.string :theme_preference, null: false, default: "system"
      t.string :color_preference, null: false, default: "teal"

      t.timestamps
    end

    add_index :users, :email, unique: true
  end
end
