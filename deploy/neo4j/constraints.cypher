// Uniqueness constraints for Open Library nodes.
CREATE CONSTRAINT book_key_unique IF NOT EXISTS
FOR (b:Book)
REQUIRE b.key IS UNIQUE;

CREATE CONSTRAINT work_key_unique IF NOT EXISTS
FOR (w:Work)
REQUIRE w.key IS UNIQUE;

CREATE CONSTRAINT author_key_unique IF NOT EXISTS
FOR (a:Author)
REQUIRE a.key IS UNIQUE;
