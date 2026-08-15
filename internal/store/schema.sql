-- The database as Rails left it, byte for byte.
--
-- This is `sqlite3 storage/development.sqlite3 .schema` with two edits: the
-- statements are ordered so a table exists before anything references it, and
-- sqlite_sequence is dropped (SQLite creates that one itself the first time an
-- AUTOINCREMENT row is inserted, and CREATE TABLE on it is an error).
--
-- Nothing here may be "tidied". The production database was created by
-- ActiveRecord and is still readable by it: `kamal rollback` to the Rails image
-- has to find the schema it expects, down to the /*application='Tinystart'*/
-- comments Rails appends to its index statements. Only a fresh database ever
-- runs this file; see migrate.go.

CREATE TABLE IF NOT EXISTS "users" ("id" integer PRIMARY KEY AUTOINCREMENT NOT NULL, "admin" boolean DEFAULT FALSE NOT NULL, "approved" boolean DEFAULT FALSE NOT NULL, "color_preference" varchar DEFAULT 'teal' NOT NULL, "created_at" datetime(6) NOT NULL, "email" varchar NOT NULL, "password_digest" varchar NOT NULL, "theme_preference" varchar DEFAULT 'system' NOT NULL, "updated_at" datetime(6) NOT NULL, "columns" integer DEFAULT 1 NOT NULL);
CREATE UNIQUE INDEX "index_users_on_email" ON "users" ("email") /*application='Tinystart'*/;

CREATE TABLE IF NOT EXISTS "sessions" ("id" integer PRIMARY KEY AUTOINCREMENT NOT NULL, "created_at" datetime(6) NOT NULL, "expires_at" datetime(6), "ip_address" varchar, "updated_at" datetime(6) NOT NULL, "user_agent" varchar, "user_id" integer NOT NULL, CONSTRAINT "fk_rails_758836b4f0"
FOREIGN KEY ("user_id")
  REFERENCES "users" ("id")
);
CREATE INDEX "index_sessions_on_user_id" ON "sessions" ("user_id") /*application='Tinystart'*/;

CREATE TABLE IF NOT EXISTS "start_page_groups" ("id" integer PRIMARY KEY AUTOINCREMENT NOT NULL, "column" integer NOT NULL, "created_at" datetime(6) NOT NULL, "name" varchar NOT NULL, "position" integer NOT NULL, "updated_at" datetime(6) NOT NULL, "user_id" integer NOT NULL, CONSTRAINT "fk_rails_2b9237427a"
FOREIGN KEY ("user_id")
  REFERENCES "users" ("id")
);
CREATE INDEX "index_start_page_groups_on_user_id" ON "start_page_groups" ("user_id") /*application='Tinystart'*/;
CREATE INDEX "index_start_page_groups_on_user_id_and_column_and_position" ON "start_page_groups" ("user_id", "column", "position") /*application='Tinystart'*/;
CREATE UNIQUE INDEX "index_start_page_groups_on_user_id_and_name" ON "start_page_groups" ("user_id", "name") /*application='Tinystart'*/;

CREATE TABLE IF NOT EXISTS "start_page_items" ("id" integer PRIMARY KEY AUTOINCREMENT NOT NULL, "created_at" datetime(6) NOT NULL, "position" integer NOT NULL, "start_page_group_id" integer NOT NULL, "title" varchar NOT NULL, "updated_at" datetime(6) NOT NULL, "url" varchar NOT NULL, "visit_count" integer DEFAULT 0 NOT NULL, CONSTRAINT "fk_rails_a9b5e77cee"
FOREIGN KEY ("start_page_group_id")
  REFERENCES "start_page_groups" ("id")
);
CREATE INDEX "index_start_page_items_on_start_page_group_id_and_position" ON "start_page_items" ("start_page_group_id", "position") /*application='Tinystart'*/;
CREATE UNIQUE INDEX "index_start_page_items_on_start_page_group_id_and_url" ON "start_page_items" ("start_page_group_id", "url") /*application='Tinystart'*/;
CREATE INDEX "index_start_page_items_on_start_page_group_id" ON "start_page_items" ("start_page_group_id") /*application='Tinystart'*/;

CREATE TABLE IF NOT EXISTS "connections" ("id" integer PRIMARY KEY AUTOINCREMENT NOT NULL, "base_url" varchar NOT NULL, "created_at" datetime(6) NOT NULL, "last_error" varchar, "last_failed_at" datetime(6), "scopes" varchar, "token" varchar NOT NULL, "token_expires_at" datetime(6), "updated_at" datetime(6) NOT NULL, "user_id" integer NOT NULL, CONSTRAINT "fk_rails_648eb9fb35"
FOREIGN KEY ("user_id")
  REFERENCES "users" ("id")
);
CREATE UNIQUE INDEX "index_connections_on_user_id" ON "connections" ("user_id") /*application='Tinystart'*/;

CREATE TABLE IF NOT EXISTS "schema_migrations" ("version" varchar NOT NULL PRIMARY KEY);
CREATE TABLE IF NOT EXISTS "ar_internal_metadata" ("key" varchar NOT NULL PRIMARY KEY, "value" varchar, "created_at" datetime(6) NOT NULL, "updated_at" datetime(6) NOT NULL);
