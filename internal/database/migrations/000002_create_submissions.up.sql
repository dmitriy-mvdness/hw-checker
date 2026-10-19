CREATE TABLE submissions (
    id            INTEGER  PRIMARY KEY,
    student_code  INTEGER  NOT NULL,   -- students.code из 1С
    theme_code    TEXT     NOT NULL,
    attempt       INTEGER  NOT NULL,   -- номер попытки
    file_path     TEXT     NOT NULL,   -- относительно HOMEWORK_DIR
    original_name TEXT     NOT NULL,   -- как назвал студент
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (student_code, theme_code, attempt)
);

CREATE INDEX idx_submissions_student_theme ON submissions(student_code, theme_code);