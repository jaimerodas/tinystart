class ScopeTinylinksConnectionsToUsers < ActiveRecord::Migration[8.1]
  def up
    # A token grants access to exactly one tinylinks account, so the connection
    # belongs to the user who approved it. Without this, every tinystart user's
    # command bar searched whichever archive happened to be connected.
    #
    # Nothing is deployed yet, so any existing row is local scratch data and can
    # go rather than being guessed at.
    execute "DELETE FROM tinylinks_connections"

    add_reference :tinylinks_connections, :user,
                  null: false, foreign_key: true, index: { unique: true }
  end

  def down
    remove_reference :tinylinks_connections, :user
  end
end
