-- Rows written by ActiveRecord, captured with
-- `sqlite3 storage/development.sqlite3 ".mode insert <table>" "select * from <table>"`
-- and then edited only to replace the personal details (email, password digest,
-- IP address, API token). Every timestamp, and the 0/1 in every boolean column,
-- is exactly the text Rails put on disk.
--
-- The point of keeping them is the datetime format, which Rails does not write
-- consistently: it appends ".%06d" only when the microseconds are non-zero, so
-- connections.token_expires_at below has no fractional part while everything
-- else does. Both shapes have to read back, and anything the store writes
-- has to come out the same way, so a row keeps one shape whoever wrote it.
-- See railsTime in time.go.
--
-- Loaded on top of schema.sql by the tests; see rails_rows_test.go.

INSERT INTO users VALUES(1,1,1,'blue','2026-08-07 22:36:04.233339','someone@example.com','$2a$12$EtqmeoUoVPpd432xuXr.1u1dG9BK5oqEVVXpHUxZfLDb5VQG1leBe','system','2026-08-10 17:05:23.270058',3);

INSERT INTO start_page_groups VALUES(15,1,'2026-08-10 16:00:49.065150','Lo de siempre',0,'2026-08-10 16:00:49.065150',1);
INSERT INTO start_page_groups VALUES(17,2,'2026-08-10 16:00:49.095721','Mis proyectitos',0,'2026-08-10 16:00:49.095721',1);

INSERT INTO start_page_items VALUES(39,'2026-08-10 16:00:49.067002',0,15,'Fastmail','2026-08-10 16:00:49.067002','https://fastmail.com',0);
INSERT INTO start_page_items VALUES(40,'2026-08-10 16:00:49.069485',1,15,'Feedbin','2026-08-10 16:00:49.069485','https://feedbin.com',0);
INSERT INTO start_page_items VALUES(52,'2026-08-10 16:00:49.099405',0,17,'Links Patito','2026-08-10 16:00:49.099405','https://links.pati.to',0);

INSERT INTO sessions VALUES(1,'2026-08-07 22:36:13.271065','2026-09-06 22:36:13.267704','203.0.113.7','2026-08-07 22:36:13.271065','Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.6 Safari/605.1.15',1);

INSERT INTO connections VALUES(3,'https://links.pati.to','2026-08-10 17:46:46.161865',NULL,NULL,'search,visit','a-token','2026-11-08 17:46:42','2026-08-10 17:46:46.161865',1);
