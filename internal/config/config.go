// Package config は、外から注入される設定（環境変数と設定ファイル）を、起動時に1か所でまとめて読み込み、検証する。
//
// ほかのパッケージ（アダプタなど）は環境変数や設定ファイルを直接読まず、ここで作った Config から値を受け取る。
// 不備は最初の1つで止めず、見つかったものをすべてまとめて返す（1回の実行で直すべき箇所がすべてわかるように）。
//
// 設定ファイルの項目は docs/configuration.md を参照。
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	// タイムゾーンのデータベースをバイナリに埋め込む（コンテナ（distroless）には /usr/share/zoneinfo がないため）
	_ "time/tzdata"

	"go.yaml.in/yaml/v3"

	"github.com/zaki3mymy/notify-schedule/internal/domain"
)

// EnvConfigPath は、設定ファイルのパスを渡す環境変数の名前。
const EnvConfigPath = "NOTIFY_SCHEDULE_CONFIG"

// DefaultTimezone は、設定ファイルで timezone を省略したときのタイムゾーン。
const DefaultTimezone = "Asia/Tokyo"

// Config は、起動時に読み込んで検証した設定。
type Config struct {
	// Location は、対象日を決めるタイムゾーン。
	Location *time.Location
	// Calendars は、予定を取得するカレンダー（1件以上）。
	Calendars []domain.Calendar
	// NandemoOshirase は、通知先の nandemo-oshirase の設定。
	NandemoOshirase NandemoOshirase
}

// NandemoOshirase は、nandemo-oshirase の設定。
type NandemoOshirase struct {
	// Endpoint は、通知の URL（https）。
	Endpoint string
	// APIKey は、x-api-key ヘッダーに付ける API キー。ログなどに出ないよう Secret にしている。
	APIKey Secret
}

// LookupEnv は、環境変数を読む関数（os.LookupEnv と同じ形。テストで差し替える）。
type LookupEnv func(key string) (string, bool)

// Load は、環境変数 NOTIFY_SCHEDULE_CONFIG のパスの設定ファイルを読み込み、検証する。
// 不備があれば、見つかったものをすべてまとめたエラーを返す。
func Load(lookupEnv LookupEnv) (Config, error) {
	path, ok := lookupEnv(EnvConfigPath)
	if !ok || strings.TrimSpace(path) == "" {
		return Config{}, fmt.Errorf("環境変数 %s に、設定ファイルのパスを指定してください", EnvConfigPath)
	}
	data, err := os.ReadFile(path) //nolint:gosec // パスは運用者が環境変数で指定するもので、外部の入力ではない
	if err != nil {
		return Config{}, fmt.Errorf("設定ファイル（%s）を読めませんでした: %w", EnvConfigPath, err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return Config{}, fmt.Errorf("設定ファイル（%s）に不備があります:\n%w", EnvConfigPath, err)
	}
	return cfg, nil
}

// file は、設定ファイル（YAML）の形。
type file struct {
	Timezone        string `yaml:"timezone"`
	NandemoOshirase struct {
		Endpoint string `yaml:"endpoint"`
		APIKey   string `yaml:"api_key"`
	} `yaml:"nandemo_oshirase"`
	Calendars []struct {
		ID    string `yaml:"id"`
		Label string `yaml:"label"`
	} `yaml:"calendars"`
}

// Parse は、設定ファイルの中身（YAML）を読み込み、検証する。
// 知らないキーもエラーにする（書き間違いに気づけるように）。
func Parse(data []byte) (Config, error) {
	var f file
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	// 空のファイルは、項目がすべて空として検証する（必須の項目の不備として報告する）
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, yamlError(err)
	}

	var errs []error
	addErr := func(field, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", field, fmt.Sprintf(format, args...)))
	}

	var cfg Config

	// timezone
	tz := strings.TrimSpace(f.Timezone)
	if tz == "" {
		tz = DefaultTimezone
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		addErr("timezone", "タイムゾーンとして読めません（例: %s）: %q", DefaultTimezone, tz)
	}
	cfg.Location = loc

	// nandemo_oshirase
	endpoint := strings.TrimSpace(f.NandemoOshirase.Endpoint)
	if endpoint == "" {
		addErr("nandemo_oshirase.endpoint", "必須です")
	} else if u, err := url.Parse(endpoint); err != nil || u.Scheme != "https" || u.Host == "" {
		// 値は秘密ではないが、公開したくないのでエラーに含めない
		addErr("nandemo_oshirase.endpoint", "https の URL を指定してください")
	}
	cfg.NandemoOshirase.Endpoint = endpoint

	apiKey := strings.TrimSpace(f.NandemoOshirase.APIKey)
	if apiKey == "" {
		addErr("nandemo_oshirase.api_key", "必須です")
	}
	cfg.NandemoOshirase.APIKey = Secret(apiKey)

	// calendars
	if len(f.Calendars) == 0 {
		addErr("calendars", "1件以上指定してください")
	}
	seen := map[string]int{}
	for i, c := range f.Calendars {
		field := fmt.Sprintf("calendars[%d]", i)
		id, label := strings.TrimSpace(c.ID), strings.TrimSpace(c.Label)
		if id == "" {
			addErr(field+".id", "必須です")
		} else if first, ok := seen[id]; ok {
			// ID はメールアドレスのことがあるので、値ではなく位置で示す
			addErr(field+".id", "calendars[%d] と同じ ID です", first)
		} else {
			seen[id] = i
		}
		if label == "" {
			addErr(field+".label", "必須です")
		}
		cfg.Calendars = append(cfg.Calendars, domain.Calendar{ID: id, Label: label})
	}

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

// yamlError は、YAML の読み込みのエラーを、行ごとにまとめて返す（知らないキーや型の違いは、すべて報告される）。
func yamlError(err error) error {
	var typeErr *yaml.TypeError
	if errors.As(err, &typeErr) {
		errs := make([]error, 0, len(typeErr.Errors))
		for _, e := range typeErr.Errors {
			errs = append(errs, errors.New(e))
		}
		return errors.Join(errs...)
	}
	return fmt.Errorf("YAML として読めません: %w", err)
}

// Secret は、API キーなどの秘密の値。fmt や log/slog で出力しても、値が出ないようにする。
type Secret string

const redacted = "[REDACTED]"

// Value は、秘密の値そのものを返す（アダプタに渡すときだけ使う）。
func (s Secret) Value() string { return string(s) }

// String は、値を隠した文字列を返す（%v・%s）。
func (s Secret) String() string { return redacted }

// GoString は、値を隠した文字列を返す（%#v）。
func (s Secret) GoString() string { return redacted }

// MarshalText は、値を隠した文字列を返す（JSON などに書き出したとき）。
func (s Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// LogValue は、値を隠した文字列を返す（log/slog で出力したとき）。
func (s Secret) LogValue() slog.Value { return slog.StringValue(redacted) }
