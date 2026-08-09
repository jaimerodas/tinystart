class RenameTinylinksConnectionsToConnections < ActiveRecord::Migration[8.1]
  def change
    rename_table :tinylinks_connections, :connections
  end
end
