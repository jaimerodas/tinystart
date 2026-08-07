class CreateTinylinksConnections < ActiveRecord::Migration[8.1]
  def change
    # One row, holding the token obtained from tinylinks' device flow. It lives
    # here rather than in credentials so rotating it never needs a redeploy.
    create_table :tinylinks_connections do |t|
      t.string :base_url, null: false
      t.string :token, null: false
      t.string :scopes
      t.datetime :token_expires_at
      t.datetime :last_failed_at
      t.string :last_error
      t.timestamps
    end
  end
end
