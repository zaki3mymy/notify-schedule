package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zaki3mymy/notify-schedule/internal/domain"
)

const testAPIKey = "dummy-api-key-for-test" //nolint:gosec // テスト用のダミーの値（本物の認証情報ではない）

const validYAML = `
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

func TestParseValid(t *testing.T) {
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatalf("Parse(): %v", err)
	}
	if cfg.Location.String() != "Asia/Tokyo" {
		t.Errorf("Location = %v, want Asia/Tokyo", cfg.Location)
	}
	if cfg.NandemoOshirase.Endpoint != "https://example.com/notify" {
		t.Errorf("Endpoint = %q", cfg.NandemoOshirase.Endpoint)
	}
	if cfg.NandemoOshirase.APIKey.Value() != testAPIKey {
		t.Errorf("APIKey.Value() = %q, want %q", cfg.NandemoOshirase.APIKey.Value(), testAPIKey)
	}
	want := []domain.Calendar{{ID: "work@example.com", Label: "仕事"}, {ID: "family@example.com", Label: "家族"}}
	if !slices.Equal(cfg.Calendars, want) {
		t.Errorf("Calendars = %v, want %v", cfg.Calendars, want)
	}
}

func TestParseDefaultTimezone(t *testing.T) {
	cfg, err := Parse([]byte(strings.Replace(validYAML, "timezone: Asia/Tokyo\n", "", 1)))
	if err != nil {
		t.Fatalf("Parse(): %v", err)
	}
	if cfg.Location.String() != DefaultTimezone {
		t.Errorf("Location = %v, want %s", cfg.Location, DefaultTimezone)
	}
}

func TestParseInvalid(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		// エラーに含まれるべき文字列（どの項目が悪いかがわかること）
		want []string
	}{
		{
			name: "空のファイル",
			yaml: "",
			want: []string{"nandemo_oshirase.endpoint: 必須です", "nandemo_oshirase.api_key: 必須です", "calendars: 1件以上"},
		},
		{
			name: "タイムゾーンが読めない",
			yaml: strings.Replace(validYAML, "Asia/Tokyo", "Asia/Nowhere", 1),
			want: []string{"timezone:", "Asia/Nowhere"},
		},
		{
			name: "エンドポイントが https でない",
			yaml: strings.Replace(validYAML, "https://example.com/notify", "http://example.com/notify", 1),
			want: []string{"nandemo_oshirase.endpoint: https の URL"},
		},
		{
			name: "エンドポイントが URL でない",
			yaml: strings.Replace(validYAML, "https://example.com/notify", "example.com", 1),
			want: []string{"nandemo_oshirase.endpoint: https の URL"},
		},
		{
			name: "API キーが空白だけ",
			yaml: strings.Replace(validYAML, testAPIKey, `"  "`, 1),
			want: []string{"nandemo_oshirase.api_key: 必須です"},
		},
		{
			name: "カレンダーの ID とラベルがない",
			yaml: strings.Replace(strings.Replace(validYAML, "id: family@example.com", "id: ''", 1), "label: 仕事", "label: ''", 1),
			want: []string{"calendars[1].id: 必須です", "calendars[0].label: 必須です"},
		},
		{
			name: "カレンダーの ID が重複している",
			yaml: strings.Replace(validYAML, "family@example.com", "work@example.com", 1),
			want: []string{"calendars[1].id: calendars[0] と同じ ID です"},
		},
		{
			name: "知らないキー（書き間違い）",
			yaml: strings.Replace(validYAML, "api_key:", "apikey:", 1),
			want: []string{"apikey", "not found"},
		},
		{
			name: "型が違う",
			yaml: strings.Replace(validYAML, "calendars:\n  - id: work@example.com\n    label: 仕事\n  - id: family@example.com\n    label: 家族\n", "calendars: work@example.com\n", 1),
			want: []string{"cannot unmarshal"},
		},
		{
			name: "YAML として読めない",
			yaml: "calendars: [",
			want: []string{"YAML として読めません"},
		},
		{
			name: "複数の不備をすべて報告する",
			yaml: `
timezone: Asia/Nowhere
nandemo_oshirase:
  endpoint: ftp://example.com
calendars:
  - id: a@example.com
`,
			want: []string{"timezone:", "nandemo_oshirase.endpoint:", "nandemo_oshirase.api_key: 必須です", "calendars[0].label: 必須です"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil {
				t.Fatal("Parse() succeeded, want error")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("エラーに %q が含まれていない:\n%v", w, err)
				}
			}
			// 秘密の値（API キー）と、カレンダーの ID は、エラーに含めない
			for _, secret := range []string{testAPIKey, "family@example.com"} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("エラーに %q が含まれている:\n%v", secret, err)
				}
			}
		})
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(valid, []byte(validYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	invalid := filepath.Join(dir, "invalid.yaml")
	if err := os.WriteFile(invalid, []byte("calendars: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	env := func(vars map[string]string) LookupEnv {
		return func(key string) (string, bool) {
			v, ok := vars[key]
			return v, ok
		}
	}

	t.Run("設定ファイルを読める", func(t *testing.T) {
		cfg, err := Load(env(map[string]string{EnvConfigPath: valid}))
		if err != nil {
			t.Fatalf("Load(): %v", err)
		}
		if len(cfg.Calendars) != 2 {
			t.Errorf("len(Calendars) = %d, want 2", len(cfg.Calendars))
		}
	})

	tests := []struct {
		name string
		vars map[string]string
		want string
	}{
		{name: "環境変数がない", vars: map[string]string{}, want: EnvConfigPath},
		{name: "環境変数が空", vars: map[string]string{EnvConfigPath: " "}, want: EnvConfigPath},
		{name: "ファイルがない", vars: map[string]string{EnvConfigPath: filepath.Join(dir, "missing.yaml")}, want: "読めませんでした"},
		{name: "中身に不備がある", vars: map[string]string{EnvConfigPath: invalid}, want: "calendars: 1件以上"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(env(tt.vars))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Load() error = %v, want to contain %q", err, tt.want)
			}
		})
	}
}

func TestSecretIsNotPrinted(t *testing.T) {
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatalf("Parse(): %v", err)
	}

	var outputs []string
	outputs = append(outputs,
		fmt.Sprint(cfg), fmt.Sprintf("%v", cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg),
		fmt.Sprintf("%s", cfg.NandemoOshirase.APIKey), //nolint:staticcheck // %s での出力を確かめる
	)
	// API キーを含む構造体を JSON にしても、値が出ないことを確かめる
	b, err := json.Marshal(cfg.NandemoOshirase) //nolint:gosec // 値が隠れることを確かめるテスト
	if err != nil {
		t.Fatal(err)
	}
	outputs = append(outputs, string(b))

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Info("設定", "api_key", cfg.NandemoOshirase.APIKey, "nandemo_oshirase", cfg.NandemoOshirase)
	textLogger := slog.New(slog.NewTextHandler(&buf, nil))
	textLogger.Info("設定", "api_key", cfg.NandemoOshirase.APIKey, "config", cfg)
	outputs = append(outputs, buf.String())

	for _, out := range outputs {
		if strings.Contains(out, testAPIKey) {
			t.Errorf("API キーが出力されている: %s", out)
		}
	}
}

// 例の設定ファイル（config.example.yaml）が、検証を通ること（例がコードとずれないように）
func TestExampleFileIsValid(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(data); err != nil {
		t.Errorf("config.example.yaml が検証を通らない: %v", err)
	}
}
