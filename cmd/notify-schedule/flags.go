package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/zaki3mymy/notify-schedule/internal/domain"
)

// options は、CLI のフラグ。
type options struct {
	// date は、対象日（YYYY-MM-DD）。空なら offsetDays で決める。
	date string
	// offsetDays は、「今日」から何日後を対象日にするか（既定は 1 = 明日）。
	offsetDays int
	// offsetDaysSet は、--offset-days が指定されたか（--date との同時指定を見分けるため）。
	offsetDaysSet bool
	// calendars は、--calendar で絞り込むカレンダー（ID かラベル）。空なら設定のすべて。
	calendars []string
	// dryRun は、nandemo-oshirase に dry_run で送るか（LINE には送らない）。
	dryRun bool
	// validateConfig は、フラグと設定の検証だけを行って終わるか。
	validateConfig bool
}

// stringList は、何回でも指定できるフラグ（--calendar a --calendar b）。
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ",") }

func (l *stringList) Set(v string) error {
	*l = append(*l, v)
	return nil
}

// parseFlags は、フラグを読み、それだけで判断できる不備を確かめる（設定が要るものは run で確かめる）。
// -h・--help のときは、使い方を errOut に出して flag.ErrHelp を返す。
func parseFlags(args []string, errOut io.Writer) (options, error) {
	var opts options
	fs := flag.NewFlagSet("notify-schedule", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.StringVar(&opts.date, "date", "", "対象日（YYYY-MM-DD）。--offset-days とは同時に指定できない")
	fs.IntVar(&opts.offsetDays, "offset-days", 1, "設定のタイムゾーンでの「今日」から何日後を対象日にするか（0 で今日）")
	fs.Var((*stringList)(&opts.calendars), "calendar", "予定を取得するカレンダーを、設定の ID かラベルで絞り込む（何回でも指定できる）")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "nandemo-oshirase に dry_run で送る（LINE には送らない）")
	fs.BoolVar(&opts.validateConfig, "validate-config", false, "フラグと設定の検証だけを行って終わる（外部の API には触れない）")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return options{}, err
		}
		return options{}, fmt.Errorf("フラグを読めませんでした: %w", err)
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "offset-days" {
			opts.offsetDaysSet = true
		}
	})

	var errs []error
	if fs.NArg() > 0 {
		errs = append(errs, fmt.Errorf("フラグ以外の引数は受け付けません: %q", fs.Args()))
	}
	if opts.date != "" && opts.offsetDaysSet {
		errs = append(errs, errors.New("--date と --offset-days は同時に指定できません"))
	}
	if opts.date != "" {
		// 形だけを先に確かめる（タイムゾーンは設定を読んでから決まるので、ここでは UTC で読む）
		if _, err := domain.ParseDate(opts.date, time.UTC); err != nil {
			errs = append(errs, fmt.Errorf("--date: %w", err))
		}
	}
	for i, c := range opts.calendars {
		if strings.TrimSpace(c) == "" {
			errs = append(errs, fmt.Errorf("--calendar（%d 番目）: 空です", i+1))
		}
	}
	return opts, errors.Join(errs...)
}

// targetDate は、対象日を決める（--date があればそれ、なければ「loc での今日 + offsetDays」）。
func (o options) targetDate(now time.Time, loc *time.Location) (domain.Date, error) {
	if o.date != "" {
		d, err := domain.ParseDate(o.date, loc)
		if err != nil {
			return domain.Date{}, fmt.Errorf("--date: %w", err)
		}
		return d, nil
	}
	d, err := domain.DateFromNow(now, loc, o.offsetDays)
	if err != nil {
		return domain.Date{}, fmt.Errorf("--offset-days: %w", err)
	}
	return d, nil
}

// selectCalendars は、--calendar の指定（ID かラベル）で、設定のカレンダーを絞り込む。
// 指定がなければ、設定のすべてを返す。設定にないものを指定したらエラーにする
// （ID はメールアドレスのことがあるので、エラーには値ではなく何番目の指定かを書く）。
func (o options) selectCalendars(all []domain.Calendar) ([]domain.Calendar, error) {
	if len(o.calendars) == 0 {
		return all, nil
	}
	var (
		selected []domain.Calendar
		errs     []error
		added    = map[string]bool{}
	)
	for i, want := range o.calendars {
		want = strings.TrimSpace(want)
		found := false
		for _, c := range all {
			if c.ID != want && c.Label != want {
				continue
			}
			found = true
			if !added[c.ID] {
				selected = append(selected, c)
				added[c.ID] = true
			}
		}
		if !found {
			errs = append(errs, fmt.Errorf("--calendar（%d 番目）: 設定ファイルにない ID かラベルです", i+1))
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return selected, nil
}
