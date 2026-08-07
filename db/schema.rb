# This file is auto-generated from the current state of the database. Instead
# of editing this file, please use the migrations feature of Active Record to
# incrementally modify your database, and then regenerate this schema definition.
#
# This file is the source Rails uses to define your schema when running `bin/rails
# db:schema:load`. When creating a new database, `bin/rails db:schema:load` tends to
# be faster and is potentially less error prone than running all of your
# migrations from scratch. Old migrations may fail to apply correctly if those
# migrations use external dependencies or application code.
#
# It's strongly recommended that you check this file into your version control system.

ActiveRecord::Schema[8.1].define(version: 2026_08_07_140000) do
  create_table "sessions", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.datetime "expires_at"
    t.string "ip_address"
    t.datetime "updated_at", null: false
    t.string "user_agent"
    t.integer "user_id", null: false
    t.index ["user_id"], name: "index_sessions_on_user_id"
  end

  create_table "start_page_groups", force: :cascade do |t|
    t.integer "column", null: false
    t.datetime "created_at", null: false
    t.string "name", null: false
    t.integer "position", null: false
    t.integer "start_page_id", null: false
    t.datetime "updated_at", null: false
    t.index ["start_page_id", "column", "position"], name: "idx_on_start_page_id_column_position_daed7dd0d0"
    t.index ["start_page_id", "name"], name: "index_start_page_groups_on_start_page_id_and_name", unique: true
    t.index ["start_page_id"], name: "index_start_page_groups_on_start_page_id"
  end

  create_table "start_page_items", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.integer "position", null: false
    t.integer "start_page_group_id", null: false
    t.string "title", null: false
    t.datetime "updated_at", null: false
    t.string "url", null: false
    t.integer "visit_count", default: 0, null: false
    t.index ["start_page_group_id", "position"], name: "index_start_page_items_on_start_page_group_id_and_position"
    t.index ["start_page_group_id", "url"], name: "index_start_page_items_on_start_page_group_id_and_url", unique: true
    t.index ["start_page_group_id"], name: "index_start_page_items_on_start_page_group_id"
  end

  create_table "start_pages", force: :cascade do |t|
    t.integer "columns", default: 3, null: false
    t.datetime "created_at", null: false
    t.string "name", null: false
    t.datetime "updated_at", null: false
    t.integer "user_id", null: false
    t.index ["user_id"], name: "index_start_pages_on_user_id", unique: true
  end

  create_table "tinylinks_connections", force: :cascade do |t|
    t.string "base_url", null: false
    t.datetime "created_at", null: false
    t.string "last_error"
    t.datetime "last_failed_at"
    t.string "scopes"
    t.string "token", null: false
    t.datetime "token_expires_at"
    t.datetime "updated_at", null: false
  end

  create_table "users", force: :cascade do |t|
    t.boolean "admin", default: false, null: false
    t.boolean "approved", default: false, null: false
    t.string "color_preference", default: "teal", null: false
    t.datetime "created_at", null: false
    t.string "email", null: false
    t.string "password_digest", null: false
    t.string "theme_preference", default: "system", null: false
    t.datetime "updated_at", null: false
    t.index ["email"], name: "index_users_on_email", unique: true
  end

  add_foreign_key "sessions", "users"
  add_foreign_key "start_page_groups", "start_pages"
  add_foreign_key "start_page_items", "start_page_groups"
  add_foreign_key "start_pages", "users"
end
