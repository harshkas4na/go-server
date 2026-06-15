-- This table name MUST match your Go code's SELECT/INSERT statements
CREATE TABLE IF NOT EXISTS todos (
    id SERIAL PRIMARY KEY,    -- SERIAL makes it auto-increment (1, 2, 3...)
    title TEXT NOT NULL       -- Matches your 'title' field
);

-- Optional: Add a starting item so you know it's working
INSERT INTO todos (title) VALUES ('Build my first AWS project');