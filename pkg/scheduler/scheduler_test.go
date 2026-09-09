package scheduler_test

import (
	"sync/atomic"
	"testing"
	"time"

	"maps-scraper/pkg/scheduler"
)

func TestDailyScheduler_IdempotencyAndConcurrencyLock(t *testing.T) {
	var callCount int32

	slowTrigger := func(triggerType string) error {
		atomic.AddInt32(&callCount, 1)
		time.Sleep(100 * time.Millisecond) // simulate active scraping
		return nil
	}

	cfg := scheduler.Config{
		Enabled:       true,
		DailyAtHour:   1,
		DailyAtMinute: 0,
	}

	sched := scheduler.NewDailyScheduler(cfg, slowTrigger)

	// Trigger 1 (should start)
	go func() {
		_, _ = sched.ExecuteRun("test1")
	}()

	time.Sleep(20 * time.Millisecond)

	// Trigger 2 while Trigger 1 is running (should be skipped due to safety lock)
	executed, err := sched.ExecuteRun("test2")
	if executed {
		t.Errorf("expected concurrent trigger to be skipped, but executed returned true")
	}
	if err == nil {
		t.Errorf("expected error indicating job is already running")
	}

	time.Sleep(150 * time.Millisecond)

	// Trigger 3 after Trigger 1 finishes (should succeed)
	executed3, err3 := sched.ExecuteRun("test3")
	if !executed3 || err3 != nil {
		t.Errorf("expected trigger 3 to execute normally after previous job finished, err: %v", err3)
	}

	total := atomic.LoadInt32(&callCount)
	if total != 2 {
		t.Errorf("expected exactly 2 executions, got %d", total)
	}
}

func TestDailyScheduler_UpdateScheduleAndPersistence(t *testing.T) {
	tempFile := t.TempDir() + "/sched_cfg.json"

	cfg := scheduler.Config{
		Enabled:       true,
		DailyAtHour:   1,
		DailyAtMinute: 0,
	}

	sched := scheduler.NewDailyScheduler(cfg, func(trigger string) error { return nil }, tempFile)

	// Verify initial status
	st := sched.GetStatus()
	if st.DailySchedule != "01:00 WIB Daily" || st.DailyAtHour != 1 || st.DailyAtMinute != 0 {
		t.Fatalf("unexpected initial status: %+v", st)
	}

	// Update to 08:30 WIB
	if err := sched.UpdateSchedule(8, 30); err != nil {
		t.Fatalf("failed to update schedule: %v", err)
	}

	st2 := sched.GetStatus()
	if st2.DailySchedule != "08:30 WIB Daily" || st2.DailyAtHour != 8 || st2.DailyAtMinute != 30 {
		t.Fatalf("unexpected updated status: %+v", st2)
	}

	// Verify persistence: create new instance pointing to same file
	sched2 := scheduler.NewDailyScheduler(cfg, func(trigger string) error { return nil }, tempFile)
	stPersisted := sched2.GetStatus()
	if stPersisted.DailySchedule != "08:30 WIB Daily" || stPersisted.DailyAtHour != 8 || stPersisted.DailyAtMinute != 30 {
		t.Fatalf("persisted status mismatch: %+v", stPersisted)
	}

	// Validation checks
	if err := sched.UpdateSchedule(25, 0); err == nil {
		t.Errorf("expected error for hour 25")
	}
	if err := sched.UpdateSchedule(12, 60); err == nil {
		t.Errorf("expected error for minute 60")
	}
	if err := sched.UpdateSchedule(-1, 0); err == nil {
		t.Errorf("expected error for hour -1")
	}
}

func TestDailyScheduler_ArticlesPerSource(t *testing.T) {
	tempFile := t.TempDir() + "/sched_articles_cfg.json"

	cfg := scheduler.Config{
		Enabled:           true,
		DailyAtHour:       1,
		DailyAtMinute:     0,
		ArticlesPerSource: 100,
	}

	sched := scheduler.NewDailyScheduler(cfg, func(trigger string) error { return nil }, tempFile)

	if sched.GetArticlesPerSource() != 100 {
		t.Fatalf("expected initial ArticlesPerSource 100, got %d", sched.GetArticlesPerSource())
	}

	if err := sched.UpdateArticlesPerSource(150); err != nil {
		t.Fatalf("failed to update ArticlesPerSource: %v", err)
	}

	if sched.GetArticlesPerSource() != 150 {
		t.Fatalf("expected updated ArticlesPerSource 150, got %d", sched.GetArticlesPerSource())
	}

	// Verify persistence
	sched2 := scheduler.NewDailyScheduler(cfg, func(trigger string) error { return nil }, tempFile)
	if sched2.GetArticlesPerSource() != 150 {
		t.Fatalf("expected persisted ArticlesPerSource 150, got %d", sched2.GetArticlesPerSource())
	}

	if err := sched.UpdateArticlesPerSource(0); err == nil {
		t.Errorf("expected error updating ArticlesPerSource to 0")
	}
}


