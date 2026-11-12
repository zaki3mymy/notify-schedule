package domain

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

// MaxMessageLength は、1件のメッセージの長さの上限（通知先が1件のメッセージで受け付ける長さ）。
//
// 長さは UTF-16 のコード単位で数える（絵文字などは2と数える）。通知先がどの単位で数えるかに
// かかわらず上限を超えないよう、多めに数える方に合わせている。
const MaxMessageLength = 5000

// Message は、通知の1件。空でなく、MaxMessageLength を超えないことを NewMessage で守る。
type Message struct {
	text string
}

// NewMessage は、テキストから Message を作る。空のとき、長さが上限を超えるときはエラーを返す。
func NewMessage(text string) (Message, error) {
	if text == "" {
		return Message{}, errors.New("メッセージが空です")
	}
	if n := messageLength(text); n > MaxMessageLength {
		return Message{}, fmt.Errorf("メッセージが長すぎます（%d、上限 %d）", n, MaxMessageLength)
	}
	return Message{text: text}, nil
}

// Text はメッセージのテキストを返す。
func (m Message) Text() string { return m.text }

// messageLength は、テキストの長さを UTF-16 のコード単位で返す。
func messageLength(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// 表示に使う文言
const (
	untitled      = "(タイトルなし)"
	allDayMark    = "[終日]"
	continuedMark = "（続き）"
	ellipsis      = "…"
	timeLayout    = "15:04"
)

var weekdays = [...]string{"日", "月", "火", "水", "木", "金", "土"}

// ComposeMessages は、ある1日の予定を、通知するメッセージに変換する。
//
// すべてのカレンダーの予定を1通にまとめる。予定が1件もないときは空を返す（＝通知しない）。
// 1通が MaxMessageLength を超えるときは、予定の行の区切りで複数のメッセージに分ける（1つの予定の途中では切らない）。
// 2通目以降の見出しには「（続き）」を付ける。
//
//	11/12(木) の予定
//
//	[終日] 誕生日（家族）
//	09:00-10:00 打ち合わせ（仕事）
func ComposeMessages(s DailySchedule) []Message {
	if s.IsEmpty() {
		return nil
	}

	heading := fmt.Sprintf("%d/%d(%s) の予定", s.Date.Month(), s.Date.Day(), weekdays[s.Date.Weekday()])
	headings := [2]string{heading + "\n", heading + continuedMark + "\n"}

	// 1行だけで上限を超える予定は、長い方の見出し（続き）と1通に収まるよう、末尾を切り詰める
	maxLine := MaxMessageLength - messageLength(headings[1]) - 1

	// 見出しと空行のあとに、予定を1行ずつ足す。足すと上限を超えるときは、次のメッセージに移る
	var texts []string
	var b strings.Builder
	for _, e := range s.SortedEvents() {
		line := fitLine(formatEvent(s, e), maxLine)
		if b.Len() > 0 && messageLength(b.String())+1+messageLength(line) > MaxMessageLength {
			texts = append(texts, b.String())
			b.Reset()
		}
		if b.Len() == 0 {
			b.WriteString(headings[min(len(texts), 1)])
		}
		b.WriteString("\n")
		b.WriteString(line)
	}
	texts = append(texts, b.String())

	messages := make([]Message, 0, len(texts))
	for _, t := range texts {
		m, err := NewMessage(t)
		if err != nil {
			// 上の組み立てで上限を超えないようにしているので、ここには来ない
			panic(fmt.Sprintf("メッセージの組み立てに誤りがあります: %v", err))
		}
		messages = append(messages, m)
	}
	return messages
}

// formatEvent は、予定を1行の文字列にする（例: 「09:00-10:00 打ち合わせ（仕事）」）。
func formatEvent(s DailySchedule, e Event) string {
	title := e.Title
	if strings.TrimSpace(title) == "" {
		title = untitled
	}
	// 改行を含むタイトルで行が崩れないようにする
	title = strings.Join(strings.Fields(title), " ")
	line := formatTime(s, e) + " " + title
	if e.CalendarLabel != "" {
		line += "（" + e.CalendarLabel + "）"
	}
	return line
}

// formatTime は、予定の時刻の部分を返す。
//
//	[終日]       終日予定、または前日から翌日まで続く予定
//	09:00-10:00  その日の中で始まって終わる予定
//	09:00        開始と終了が同じ予定
//	〜10:00      前日から続く予定
//	22:00〜      翌日まで続く予定
func formatTime(s DailySchedule, e Event) string {
	if s.coversWholeDay(e) {
		return allDayMark
	}
	loc := s.Date.Location()
	start, end := e.Start.In(loc), e.End.In(loc)
	startsBefore := start.Before(s.Date.Start())
	endsAfter := end.After(s.Date.End())

	switch {
	case startsBefore:
		return "〜" + end.Format(timeLayout)
	case endsAfter:
		return start.Format(timeLayout) + "〜"
	case !end.After(start):
		return start.Format(timeLayout)
	default:
		return start.Format(timeLayout) + "-" + end.Format(timeLayout)
	}
}

// fitLine は、行が max を超えるときに、末尾を切り詰めて「…」を付ける。
func fitLine(line string, max int) string {
	if messageLength(line) <= max {
		return line
	}
	limit := max - messageLength(ellipsis)
	var b strings.Builder
	n := 0
	for _, r := range line {
		l := utf16.RuneLen(r)
		if n+l > limit {
			break
		}
		b.WriteRune(r)
		n += l
	}
	return b.String() + ellipsis
}
