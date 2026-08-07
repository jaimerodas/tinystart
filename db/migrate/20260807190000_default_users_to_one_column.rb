class DefaultUsersToOneColumn < ActiveRecord::Migration[8.1]
  # A brand new start page has nothing in it, and three empty columns of nothing
  # read as a broken page. Start at one and let people widen it from Settings.
  #
  # Only affects rows inserted from here on. Existing users keep the value they
  # have — including anyone who never had a start_pages row and so inherited the
  # previous migration's default of 3 rather than picking it. They can set it to
  # 1 in Settings; deliberately backfilling them is not worth the risk of
  # overwriting a real choice, since the two are indistinguishable now.
  def change
    change_column_default :users, :columns, from: 3, to: 1
  end
end
