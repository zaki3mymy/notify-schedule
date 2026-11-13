// Package nandemooshirase は、nandemo-oshirase の API で通知する Notifier（usecase.Notifier の実装）。
//
// nandemo-oshirase: https://github.com/zaki3mymy/nandemo-oshirase
//
// エンドポイントと API キーは、コンストラクタの引数で受け取る。このパッケージの中では環境変数も
// 設定ファイルも読まない（読み込みと検証は、起動時に config パッケージでまとめて行う）。
package nandemooshirase

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zaki3mymy/notify-schedule/internal/domain"
)

// DefaultTimeout は、Config.Timeout を指定しなかったときの、1回のリクエストのタイムアウト。
const DefaultTimeout = 30 * time.Second

// maxErrorBody は、エラーのときに読むレスポンスの本文の上限（64 KiB）。
// 大きな本文でメモリを使いすぎないようにする。
const maxErrorBody = 64 * 1024

// Config は、Notifier の設定。
type Config struct {
	// Endpoint は、nandemo-oshirase の通知の URL（例: https://.../notify）。
	Endpoint string
	// APIKey は、x-api-key ヘッダーに付ける API キー。
	APIKey string
	// DryRun が true のときは、nandemo-oshirase に dry_run を付けて送る（LINE には送られない）。
	DryRun bool
	// Timeout は、1回のリクエストのタイムアウト。0 なら DefaultTimeout。
	Timeout time.Duration
	// HTTPClient は、リクエストに使うクライアント（テスト用）。nil なら Timeout を設定した http.Client を使う。
	HTTPClient *http.Client
}

// Notifier は、nandemo-oshirase の API で通知する。
type Notifier struct {
	endpoint string
	apiKey   string
	dryRun   bool
	client   *http.Client
}

// New は、Notifier を作る。Endpoint と APIKey は必須。
func New(cfg Config) (*Notifier, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("nandemo-oshirase のエンドポイントが指定されていません")
	}
	if cfg.APIKey == "" {
		return nil, errors.New("nandemo-oshirase の API キーが指定されていません")
	}
	client := cfg.HTTPClient
	if client == nil {
		timeout := cfg.Timeout
		if timeout == 0 {
			timeout = DefaultTimeout
		}
		client = &http.Client{Timeout: timeout}
	}
	return &Notifier{endpoint: cfg.Endpoint, apiKey: cfg.APIKey, dryRun: cfg.DryRun, client: client}, nil
}

// String は、API キーを含めずに Notifier を表す（ログに API キーを出さないため）。
func (n *Notifier) String() string {
	return fmt.Sprintf("nandemooshirase.Notifier{endpoint: %s, dryRun: %t}", n.endpoint, n.dryRun)
}

// request は、nandemo-oshirase の POST /notify のリクエストの本文。
type request struct {
	Messages []string `json:"messages"`
	// dry_run は true のときだけ送る（nandemo-oshirase の既定は false）
	DryRun bool `json:"dry_run,omitempty"`
}

// errorResponse は、nandemo-oshirase がエラーのときに返す本文。
type errorResponse struct {
	Error string `json:"error"`
}

// Notify は、メッセージをまとめて1回のリクエストで送る。メッセージがないときは何もしない。
//
// 失敗しても再送しない。タイムアウトのときは、nandemo-oshirase 側では送れている可能性があるため
// （再送すると二重に届くおそれがある）。2xx 以外のときは、ステータスコードと本文の error をエラーに含める。
func (n *Notifier) Notify(ctx context.Context, msgs []domain.Message) error {
	if len(msgs) == 0 {
		return nil
	}

	texts := make([]string, 0, len(msgs))
	for _, m := range msgs {
		texts = append(texts, m.Text())
	}
	body, err := json.Marshal(request{Messages: texts, DryRun: n.dryRun})
	if err != nil {
		return fmt.Errorf("nandemo-oshirase へのリクエストを作れませんでした: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("nandemo-oshirase へのリクエストを作れませんでした: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", n.apiKey)
	req.Header.Set("User-Agent", "notify-schedule")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("nandemo-oshirase に送れませんでした: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return &StatusError{StatusCode: resp.StatusCode, Message: errorMessage(resp.Body)}
}

// StatusError は、nandemo-oshirase が 2xx 以外を返したときのエラー。
type StatusError struct {
	StatusCode int
	// Message は、本文の error（JSON でないときは本文の先頭）。
	Message string
}

func (e *StatusError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("nandemo-oshirase が %d を返しました", e.StatusCode)
	}
	return fmt.Sprintf("nandemo-oshirase が %d を返しました: %s", e.StatusCode, e.Message)
}

// errorMessage は、エラーのレスポンスの本文から、エラーの内容を取り出す。
func errorMessage(body io.Reader) string {
	b, err := io.ReadAll(io.LimitReader(body, maxErrorBody))
	if err != nil {
		return ""
	}
	var resp errorResponse
	if json.Unmarshal(b, &resp) == nil && resp.Error != "" {
		return resp.Error
	}
	// JSON でないとき（API Gateway が返す本文など）は、先頭だけを1行にして返す
	s := strings.Join(strings.Fields(string(b)), " ")
	const maxLen = 200
	if r := []rune(s); len(r) > maxLen {
		s = string(r[:maxLen]) + "…"
	}
	return s
}
