CREATE TABLE accounts (
    account_id      INTEGER  PRIMARY KEY,
    user_id         INTEGER  NOT NULL UNIQUE,   --telegram user id
    personal_code   TEXT     NOT NULL UNIQUE,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);