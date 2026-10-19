// rotate.go — io.Writer с ротацией логов по дате и размеру.
package logger

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DailyWriter — io.Writer с ротацией логов по дате и размеру.
//
//	bot-YYYY-MM-DD.log        — текущий файл дня
//	bot-YYYY-MM-DD-N.log      — части, если файл дня превысил maxSize
//	bot-YYYY-MM-DD.log.gz     — закрытый файл после сжатия
//
// Ротация: при смене даты или превышении maxSize текущий файл закрывается
// (и сжимается, если compress=true), открывается следующий. Уборка старых
// файлов идёт после каждой ротации: удаляются старше maxAge и лишние сверх
// maxFiles.
//
// Реализует io.Writer и io.Closer, потокобезопасен.
type DailyWriter struct {
	dir      string
	maxSize  int64
	maxFiles int
	maxAge   time.Duration
	compress bool

	mu    sync.Mutex
	file  *os.File
	date  string
	index int
	size  int64
}

// NewDailyWriter создаёт writer и сразу готовит папку. Если папки нет —
// создаёт. Проверяет права на запись через пробный файл.
func NewDailyWriter(dir string, maxSizeMB int64, maxFiles, maxAgeDays int, compress bool) (*DailyWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}

	probe := filepath.Join(dir, ".write-test")
	if err := os.WriteFile(probe, nil, 0o644); err != nil {
		return nil, fmt.Errorf("нет прав на запись в %s: %w", dir, err)
	}
	_ = os.Remove(probe)

	return &DailyWriter{
		dir:      dir,
		maxSize:  maxSizeMB * 1024 * 1024,
		maxFiles: maxFiles,
		maxAge:   time.Duration(maxAgeDays) * 24 * time.Hour,
		compress: compress,
	}, nil
}

// Write пишет данные в текущий файл, ротируя при необходимости.
func (w *DailyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	today := time.Now().Format("2006-01-02")

	// Смена дня — закрываем старый файл.
	if w.date != "" && w.date != today {
		if err := w.closeAndCompress(); err != nil {
			fmt.Fprintf(os.Stderr, "log rotate (new day): %v\n", err)
		}
		w.file = nil
		w.date = ""
		w.index = 0
		w.size = 0
	}

	// Файл ещё не открыт — открываем нулевую часть.
	if w.file == nil {
		if err := w.open(today, 0); err != nil {
			return 0, err
		}
	}

	// Файл дня переполнен — открываем следующую часть.
	if w.size > 0 && w.size+int64(len(p)) > w.maxSize {
		if err := w.closeAndCompress(); err != nil {
			fmt.Fprintf(os.Stderr, "log rotate (size): %v\n", err)
		}
		w.file = nil
		w.index++
		if err := w.open(today, w.index); err != nil {
			return 0, err
		}
	}

	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// Sync сбрасывает буферы текущего файла на диск.
func (w *DailyWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	return w.file.Sync()
}

// Close закрывает текущий файл, не сжимая его. Для закрытия последнего
// файла дня этого достаточно — он сожмётся при следующей ротации.
func (w *DailyWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// open открывает файл дня и его индекса.
func (w *DailyWriter) open(date string, index int) error {
	var name string
	if index == 0 {
		name = fmt.Sprintf("bot-%s.log", date)
	} else {
		name = fmt.Sprintf("bot-%s-%d.log", date, index)
	}
	path := filepath.Join(w.dir, name)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", path, err)
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("stat log file %s: %w", path, err)
	}

	w.file = f
	w.date = date
	w.index = index
	w.size = info.Size()
	return nil
}

// closeAndCompress закрывает текущий файл, сжимает (если включено) и
// запускает уборку старых.
func (w *DailyWriter) closeAndCompress() error {
	if w.file == nil {
		return nil
	}

	path := w.file.Name()
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}

	if w.compress {
		if err := gzipFile(path); err != nil {
			return fmt.Errorf("compress %s: %w", path, err)
		}
	}

	return w.cleanup()
}

// cleanup удаляет файлы старше maxAge и лишние сверх maxFiles.
func (w *DailyWriter) cleanup() error {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return fmt.Errorf("read log dir: %w", err)
	}

	type logFile struct {
		path    string
		modTime time.Time
	}

	var files []logFile
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "bot-") {
			continue
		}
		if !strings.HasSuffix(name, ".log") && !strings.HasSuffix(name, ".log.gz") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, logFile{
			path:    filepath.Join(w.dir, name),
			modTime: info.ModTime(),
		})
	}

	// Старые сверху.
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})

	cutoff := time.Now().Add(-w.maxAge)

	// Убираем по возрасту.
	var kept []logFile
	for _, f := range files {
		if f.modTime.Before(cutoff) {
			if err := os.Remove(f.path); err != nil {
				fmt.Fprintf(os.Stderr, "log cleanup remove %s: %v\n", f.path, err)
			}
			continue
		}
		kept = append(kept, f)
	}

	// Убираем по количеству.
	if w.maxFiles > 0 && len(kept) > w.maxFiles {
		excess := len(kept) - w.maxFiles
		for i := 0; i < excess; i++ {
			if err := os.Remove(kept[i].path); err != nil {
				fmt.Fprintf(os.Stderr, "log cleanup remove %s: %v\n", kept[i].path, err)
			}
		}
	}

	return nil
}

// gzipFile сжимает файл: <path> → <path>.gz. Оригинал удаляется только
// после успешного сжатия. При ошибке .gz тоже удаляется.
func gzipFile(path string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(path + ".gz")
	if err != nil {
		return err
	}

	gz := gzip.NewWriter(dst)
	if _, err := io.Copy(gz, src); err != nil {
		_ = gz.Close()
		_ = dst.Close()
		_ = os.Remove(path + ".gz")
		return err
	}
	if err := gz.Close(); err != nil {
		_ = dst.Close()
		_ = os.Remove(path + ".gz")
		return err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(path + ".gz")
		return err
	}

	return os.Remove(path)
}
