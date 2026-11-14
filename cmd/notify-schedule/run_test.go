package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/zaki3mymy/notify-schedule/internal/config"
	"github.com/zaki3mymy/notify-schedule/internal/domain"
	"github.com/zaki3mymy/notify-schedule/internal/usecase"
)

const testAPIKey = "dummy-api-key-for-test" //nolint:gosec // テスト用のダミーの値（本物の認証情報ではない）

const validConfig = `
timezone: Asia/Tokyo
nandemo_oshirase:
  endpoint: https://example.com/notify
  api_key: ` + testAPIKey + `
calendars:
  - id: work@example.com
    label: 仕事
  - id: family@example.com
    label: 家族
`

// 2026-11-12 21:00（Asia/Tokyo）。明日は 2026-11-13
var testNow = time.Date(2026, time.November, 12, 12, 0, 0, 0, time.UTC)

type fakeSource struct {
	events  []domain.Event
	err     error
	gotCals []domain.Calendar
	gotDate domain.Date
}

func (s *fakeSource) Fetch(_ context.Context, cals []domain.Calendar, d domain.Date) (domain.DailySchedule, error) {
	s.gotCals, s.gotDate = cals, d
	if s.err != nil {
		return domain.DailySchedule{}, s.err
	}
	return domain.DailySchedule{Date: d, Events: s.events}, nil
}

type fakeNotifier struct {
	err   error
	calls int
}

func (n *fakeNotifier) Notify(context.Context, []domain.Message) error {
	n.calls++
	return n.err
}

// harness は、run に渡す env と、その結果（ログ・アダプタが作られたか）をまとめる。
type harness struct {
	t          *testing.T
	configPath string
	source     *fakeSource
	notifier   *fakeNotifier
	sourceErr  error

	sourceCreated   bool
	notifierCreated bool
	gotDryRun       bool
	gotNandemo      config.NandemoOshirase
	stdout          bytes.Buffer
	stderr          bytes.Buffer
}

func newHarness(t *testing.T, configYAML string) *harness {
	t.Helper()
	h := &harness{t: t, source: &fakeSource{}, notifier: &fakeNotifier{}}
	if configYAML != "" {
		h.configPath = filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(h.configPath, []byte(configYAML), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

func (h *harness) run(args ...string) int {
	h.t.Helper()
	return run(context.Background(), args, env{
		lookupEnv: func(key string) (string, bool) {
			if key == config.EnvConfigPath && h.configPath != "" {
				return h.configPath, true
			}
			return "", false
		},
		now:    func() time.Time { return testNow },
		stdout: &h.stdout,
		stderr: &h.stderr,
		newSource: func(context.Context) (usecase.ScheduleSource, error) {
			h.sourceCreated = true
			if h.sourceErr != nil {
				return nil, h.sourceErr
			}
			return h.source, nil
		},
		newNotifier: func(cfg config.NandemoOshirase, dryRun bool) (usecase.Notifier, error) {
			h.notifierCreated = true
			h.gotNandemo, h.gotDryRun = cfg, dryRun
			return h.notifier, nil
		},
	})
}

// logs は、標準出力の JSON のログを1行ずつ読む。
func (h *harness) logs() []map[string]any {
	h.t.Helper()
	var logs []map[string]any
	sc := bufio.NewScanner(&h.stdout)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			h.t.Fatalf("ログが JSON ではありません: %v (%s)", err, sc.Text())
		}
		logs = append(logs, m)
	}
	return logs
}

// lastLog は、最後のログを返す。
func (h *harness) lastLog() map[string]any {
	h.t.Helper()
	logs := h.logs()
	if len(logs) == 0 {
		h.t.Fatal("ログがありません")
	}
	return logs[len(logs)-1]
}

func testEvents() []domain.Event {
	loc, _ := time.LoadLocation("Asia/Tokyo")
	start := time.Date(2026, time.November, 13, 9, 0, 0, 0, loc)
	return []domain.Event{{Title: "打ち合わせ", Start: start, End: start.Add(time.Hour), CalendarLabel: "仕事"}}
}

func TestRunNotifiesTomorrow(t *testing.T) {
	h := newHarness(t, validConfig)
	h.source.events = testEvents()

	if code := h.run(); code != exitOK {
		t.Fatalf("run() = %d, want %d\n%s", code, exitOK, h.stdout.String())
	}

	// 既定は、設定のタイムゾーン（Asia/Tokyo）での明日
	if h.source.gotDate.String() != "2026-11-13" || h.source.gotDate.Location().String() != "Asia/Tokyo" {
		t.Errorf("対象日 = %v (%v), want 2026-11-13 (Asia/Tokyo)", h.source.gotDate, h.source.gotDate.Location())
	}
	if len(h.source.gotCals) != 2 {
		t.Errorf("カレンダーの数 = %d, want 2", len(h.source.gotCals))
	}
	if h.notifier.calls != 1 {
		t.Errorf("Notify の呼び出し = %d 回, want 1 回", h.notifier.calls)
	}
	if h.gotDryRun {
		t.Error("--dry-run なしなのに dry_run になっている")
	}
	if h.gotNandemo.Endpoint != "https://example.com/notify" || h.gotNandemo.APIKey.Value() != testAPIKey {
		t.Errorf("通知先の設定 = %+v", h.gotNandemo)
	}

	last := h.lastLog()
	if last["severity"] != "INFO" || last["message"] != "通知しました" || last["messages"] != float64(1) {
		t.Errorf("最後のログ = %v", last)
	}
	// ログに API キーを出さない
	if strings.Contains(h.stdout.String(), testAPIKey) {
		t.Error("ログに API キーが含まれている")
	}
}

func TestRunWithFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantDate string
		wantCals []string
		wantDry  bool
	}{
		{name: "--date", args: []string{"--date", "2026-12-24"}, wantDate: "2026-12-24", wantCals: []string{"仕事", "家族"}},
		{name: "--offset-days 0（今日）", args: []string{"--offset-days", "0"}, wantDate: "2026-11-12", wantCals: []string{"仕事", "家族"}},
		{name: "--calendar を ID で", args: []string{"--calendar", "family@example.com"}, wantDate: "2026-11-13", wantCals: []string{"家族"}},
		{name: "--calendar をラベルで、重複は1つに", args: []string{"--calendar", "仕事", "--calendar", "work@example.com"}, wantDate: "2026-11-13", wantCals: []string{"仕事"}},
		{name: "--dry-run", args: []string{"--dry-run"}, wantDate: "2026-11-13", wantCals: []string{"仕事", "家族"}, wantDry: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, validConfig)
			h.source.events = testEvents()
			if code := h.run(tt.args...); code != exitOK {
				t.Fatalf("run() = %d, want %d\n%s", code, exitOK, h.stdout.String())
			}
			if h.source.gotDate.String() != tt.wantDate {
				t.Errorf("対象日 = %s, want %s", h.source.gotDate, tt.wantDate)
			}
			var labels []string
			for _, c := range h.source.gotCals {
				labels = append(labels, c.Label)
			}
			if !slices.Equal(labels, tt.wantCals) {
				t.Errorf("カレンダー = %v, want %v", labels, tt.wantCals)
			}
			if h.gotDryRun != tt.wantDry {
				t.Errorf("dry_run = %v, want %v", h.gotDryRun, tt.wantDry)
			}
		})
	}
}

func TestRunDoesNotNotifyWhenNoEvents(t *testing.T) {
	h := newHarness(t, validConfig)
	if code := h.run(); code != exitOK {
		t.Fatalf("run() = %d, want %d", code, exitOK)
	}
	if h.notifier.calls != 0 {
		t.Errorf("予定がないのに Notify が %d 回呼ばれた", h.notifier.calls)
	}
	if last := h.lastLog(); last["message"] != "予定がないので、通知しませんでした" {
		t.Errorf("最後のログ = %v", last)
	}
}

// 設定やフラグに不備があるときは、アダプタを作らず（外部の API に触れず）に、不備をすべて出して終わる
func TestRunInvalidDoesNotTouchExternalAPIs(t *testing.T) {
	tests := []struct {
		name   string
		config string
		args   []string
		want   []string
	}{
		{
			name: "設定ファイルのパスがない",
			args: nil,
			want: []string{config.EnvConfigPath},
		},
		{
			name:   "設定ファイルの中身に不備がある",
			config: "calendars: []\n",
			want:   []string{"nandemo_oshirase.endpoint: 必須です", "calendars: 1件以上"},
		},
		{
			name:   "--date と --offset-days を同時に指定",
			config: validConfig,
			args:   []string{"--date", "2026-11-13", "--offset-days", "2"},
			want:   []string{"同時に指定できません"},
		},
		{
			name:   "--date の形が違う",
			config: validConfig,
			args:   []string{"--date", "2026/11/13"},
			want:   []string{"--date:"},
		},
		{
			name:   "--calendar が設定にない",
			config: validConfig,
			args:   []string{"--calendar", "仕事", "--calendar", "unknown@example.com"},
			want:   []string{"--calendar（2 番目）: 設定ファイルにない"},
		},
		{
			name:   "知らないフラグ",
			config: validConfig,
			args:   []string{"--unknown"},
			want:   []string{"フラグを読めませんでした"},
		},
		{
			name:   "フラグ以外の引数",
			config: validConfig,
			args:   []string{"extra"},
			want:   []string{"フラグ以外の引数"},
		},
		{
			name:   "フラグの不備と --calendar の不備を、まとめて出す",
			config: validConfig,
			args:   []string{"--date", "2026/11/13", "--offset-days", "2", "--calendar", "nobody"},
			want:   []string{"同時に指定できません", "--date:", "--calendar（1 番目）: 設定ファイルにない"},
		},
		{
			name: "フラグと設定の両方に不備があれば、両方を出す",
			args: []string{"--date", "2026/11/13"},
			want: []string{"--date:", config.EnvConfigPath},
		},
		{
			name:   "--validate-config でも、不備があれば失敗する",
			config: "calendars: []\n",
			args:   []string{"--validate-config"},
			want:   []string{"calendars: 1件以上"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, tt.config)
			if code := h.run(tt.args...); code != exitInvalid {
				t.Errorf("run() = %d, want %d", code, exitInvalid)
			}
			if h.sourceCreated || h.notifierCreated {
				t.Errorf("不備があるのにアダプタが作られた（source=%v, notifier=%v）", h.sourceCreated, h.notifierCreated)
			}
			last := h.lastLog()
			if last["severity"] != "ERROR" {
				t.Errorf("severity = %v, want ERROR", last["severity"])
			}
			problems, _ := json.Marshal(last["problems"])
			for _, w := range tt.want {
				if !strings.Contains(string(problems), w) {
					t.Errorf("不備の一覧に %q が含まれていない: %s", w, problems)
				}
			}
		})
	}
}

func TestRunValidateConfig(t *testing.T) {
	h := newHarness(t, validConfig)
	if code := h.run("--validate-config"); code != exitOK {
		t.Fatalf("run() = %d, want %d\n%s", code, exitOK, h.stdout.String())
	}
	if h.sourceCreated || h.notifierCreated {
		t.Errorf("--validate-config なのにアダプタが作られた（source=%v, notifier=%v）", h.sourceCreated, h.notifierCreated)
	}
	if last := h.lastLog(); last["message"] != "フラグと設定に不備はありません" {
		t.Errorf("最後のログ = %v", last)
	}
}

func TestRunFailures(t *testing.T) {
	t.Run("取得元を用意できない", func(t *testing.T) {
		h := newHarness(t, validConfig)
		h.sourceErr = errors.New("認証情報がない")
		if code := h.run(); code != exitFailure {
			t.Errorf("run() = %d, want %d", code, exitFailure)
		}
		if h.notifierCreated {
			t.Error("取得元を用意できないのに、通知先が作られた")
		}
	})
	t.Run("取得に失敗", func(t *testing.T) {
		h := newHarness(t, validConfig)
		h.source.err = errors.New("取得元のエラー")
		if code := h.run(); code != exitFailure {
			t.Errorf("run() = %d, want %d", code, exitFailure)
		}
		if h.notifier.calls != 0 {
			t.Error("取得に失敗したのに通知した")
		}
		if last := h.lastLog(); last["severity"] != "ERROR" || !strings.Contains(last["error"].(string), "取得元のエラー") {
			t.Errorf("最後のログ = %v", last)
		}
	})
	t.Run("通知に失敗", func(t *testing.T) {
		h := newHarness(t, validConfig)
		h.source.events = testEvents()
		h.notifier.err = errors.New("通知先のエラー")
		if code := h.run(); code != exitFailure {
			t.Errorf("run() = %d, want %d", code, exitFailure)
		}
	})
}

func TestRunHelp(t *testing.T) {
	h := newHarness(t, validConfig)
	if code := h.run("-h"); code != exitOK {
		t.Errorf("run(-h) = %d, want %d", code, exitOK)
	}
	if !strings.Contains(h.stderr.String(), "-validate-config") {
		t.Errorf("使い方が出ていない: %s", h.stderr.String())
	}
	if h.sourceCreated || h.notifierCreated {
		t.Error("-h なのにアダプタが作られた")
	}
}

// spanAttrs は、span の属性を map にする。
func spanAttrs(s sdktrace.ReadOnlySpan) map[attribute.Key]attribute.Value {
	m := map[attribute.Key]attribute.Value{}
	for _, kv := range s.Attributes() {
		m[kv.Key] = kv.Value
	}
	return m
}

func TestRunRecordsSpan(t *testing.T) {
	t.Run("成功", func(t *testing.T) {
		rec := recordSpans(t)
		h := newHarness(t, validConfig)
		h.source.events = testEvents()
		if code := h.run(); code != exitOK {
			t.Fatalf("run() = %d, want %d", code, exitOK)
		}

		spans := rec.Ended()
		if len(spans) != 1 || spans[0].Name() != "notify-schedule" {
			t.Fatalf("span = %v, want notify-schedule の1つ", spans)
		}
		attrs := spanAttrs(spans[0])
		if got := attrs["schedule.date"].AsString(); got != "2026-11-13" {
			t.Errorf("schedule.date = %q, want 2026-11-13", got)
		}
		if got := attrs["schedule.calendars"].AsInt64(); got != 2 {
			t.Errorf("schedule.calendars = %d, want 2", got)
		}
		if got := attrs["schedule.events"].AsInt64(); got != 1 {
			t.Errorf("schedule.events = %d, want 1", got)
		}
		if got := attrs["notify.messages"].AsInt64(); got != 1 {
			t.Errorf("notify.messages = %d, want 1", got)
		}
		if spans[0].Status().Code != codes.Unset {
			t.Errorf("status = %v, want Unset", spans[0].Status())
		}
	})
	t.Run("失敗", func(t *testing.T) {
		rec := recordSpans(t)
		h := newHarness(t, validConfig)
		h.source.err = errors.New("取得元のエラー")
		if code := h.run(); code != exitFailure {
			t.Fatalf("run() = %d, want %d", code, exitFailure)
		}

		spans := rec.Ended()
		if len(spans) != 1 {
			t.Fatalf("span の数 = %d, want 1", len(spans))
		}
		if st := spans[0].Status(); st.Code != codes.Error || !strings.Contains(st.Description, "取得元のエラー") {
			t.Errorf("status = %v, want Error（取得元のエラー）", st)
		}
	})
	t.Run("取得元を用意できない", func(t *testing.T) {
		rec := recordSpans(t)
		h := newHarness(t, validConfig)
		h.sourceErr = errors.New("認証情報がない")
		if code := h.run(); code != exitFailure {
			t.Fatalf("run() = %d, want %d", code, exitFailure)
		}
		if spans := rec.Ended(); len(spans) != 1 || spans[0].Status().Code != codes.Error {
			t.Errorf("span = %v, want Error の1つ", spans)
		}
	})
	t.Run("設定の検証だけなら span を作らない", func(t *testing.T) {
		rec := recordSpans(t)
		h := newHarness(t, validConfig)
		if code := h.run("--validate-config"); code != exitOK {
			t.Fatalf("run() = %d, want %d", code, exitOK)
		}
		if spans := rec.Ended(); len(spans) != 0 {
			t.Errorf("span の数 = %d, want 0", len(spans))
		}
	})
}
