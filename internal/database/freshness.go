// freshness.go — проверка свежести snapshot.db. Файл наполняет 1С,
// бот его только читает; если выгрузка сломалась и файл не
// обновляется, бот работает по устаревшему срезу. Об этом никто не
// узнает, пока студент не пожалуется «не вижу новую тему».
package database

import (
	"os"
	"time"

	"go.uber.org/zap"
)

// CheckSnapshotFreshness смотрит mtime файла и пишет Warn, если файл
// старше maxAge. maxAge <= 0 — проверка отключена (см.
// SNAPSHOT_MAX_AGE_HOURS=0): у кого-то выгрузка раз в неделю, и
// дефолтный порог только раздражает.
//
// Молчит, если всё в порядке: Info про возраст уже пишет Open при
// старте, дублировать его каждый раз незачем.
//
// Ошибка stat — Warn, а не Error: файл могли временно убрать (например,
// администратор перекладывает выгрузку), сам по себе это не сбой бота.
// Проверка одна, следующего шанса не будет — но и падать из-за
// отсутствия файла нельзя, Open выше уже дал явную ошибку про то, что
// snapshot не читается.
func CheckSnapshotFreshness(log *zap.Logger, path string, maxAge time.Duration) {
	if maxAge <= 0 {
		log.Info("проверка свежести snapshot отключена",
			zap.String("path", path))
		return
	}

	fi, err := os.Stat(path)
	if err != nil {
		log.Warn("не удалось проверить свежесть snapshot",
			zap.String("path", path),
			zap.Error(err),
		)
		return
	}

	age := time.Since(fi.ModTime())
	if age <= maxAge {
		return
	}

	log.Warn("snapshot.db устарел — проверьте выгрузку из 1С",
		zap.String("path", path),
		zap.Time("updated_at", fi.ModTime()),
		zap.Duration("age", age.Round(time.Hour)),
		zap.Duration("max_age", maxAge),
	)
}
