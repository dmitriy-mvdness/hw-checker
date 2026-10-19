CREATE TABLE news (
    id         INTEGER  PRIMARY KEY,
    theme_code TEXT     NOT NULL UNIQUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_news_created_at ON news(created_at DESC);

CREATE TABLE seen_themes (
    theme_code TEXT     PRIMARY KEY,
    seen_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);