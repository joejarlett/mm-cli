// Package nldate is mm-cli's natural-language date parser — the
// hand-rolled replacement for the TS side's chrono-node. Scope is
// deliberately narrow (see specs/go-port/04-nl-dates.md): just the
// phrases Joe actually types. Anything else is an error.
//
// Two entry points mirror the TS:
//   ParseDateTime(raw, now) — for --when. Returns wall-clock in local TZ.
//   ParseDate(raw, now)     — for --due. Date only (midnight local).
//
// `now` is injectable for testing; production callers pass time.Now().
package nldate

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Result is the parsed datetime + an ISO string the caller can send to
// the server. Matches the TS shape `{iso, date}`.
type Result struct {
	ISO  string
	Date time.Time
}

var errEmpty = errors.New("empty date")

// ParseDateTime parses --when style input. Returns a wall-clock time in
// `now`'s location. Output ISO is the local "YYYY-MM-DDTHH:MM:SS" form
// (no tz suffix — Google's dateTime+timeZone pair handles offset).
func ParseDateTime(raw string, now time.Time) (Result, error) {
	t := strings.TrimSpace(raw)
	if t == "" {
		return Result{}, errEmpty
	}
	if d, ok := tryISODateTime(t, now.Location()); ok {
		return Result{ISO: formatLocalDateTime(d), Date: d}, nil
	}
	if d, ok := tryISODate(t, now.Location()); ok {
		return Result{ISO: formatLocalDateTime(d), Date: d}, nil
	}
	d, err := parseRelative(t, now)
	if err != nil {
		return Result{}, fmt.Errorf("couldn't parse %q as a date/time", raw)
	}
	return Result{ISO: formatLocalDateTime(d), Date: d}, nil
}

// ParseDate parses --due style input. Same surface as ParseDateTime but
// the output ISO is "YYYY-MM-DD" (date only).
func ParseDate(raw string, now time.Time) (Result, error) {
	t := strings.TrimSpace(raw)
	if t == "" {
		return Result{}, errEmpty
	}
	if d, ok := tryISODate(t, now.Location()); ok {
		return Result{ISO: formatLocalDate(d), Date: d}, nil
	}
	d, err := parseRelative(t, now)
	if err != nil {
		return Result{}, fmt.Errorf("couldn't parse %q as a date", raw)
	}
	// Date-only output truncates to midnight; preserve full Date in result.
	return Result{ISO: formatLocalDate(d), Date: d}, nil
}

// ─── ISO fast paths ────────────────────────────────────────────────────

var (
	isoDateTimeRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[ T]\d{1,2}:\d{2}(:\d{2})?$`)
	isoDateRe     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

func tryISODateTime(s string, loc *time.Location) (time.Time, bool) {
	if !isoDateTimeRe.MatchString(s) {
		return time.Time{}, false
	}
	// Normalise: "T" or " ", optional seconds.
	s2 := strings.Replace(s, "T", " ", 1)
	layouts := []string{"2006-01-02 15:04", "2006-01-02 15:04:05"}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, s2, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func tryISODate(s string, loc *time.Location) (time.Time, bool) {
	if !isoDateRe.MatchString(s) {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02", s, loc)
	return t, err == nil
}

// ─── Relative / natural ────────────────────────────────────────────────

var weekdayMap = map[string]time.Weekday{
	"sun": time.Sunday, "sunday": time.Sunday,
	"mon": time.Monday, "monday": time.Monday,
	"tue": time.Tuesday, "tues": time.Tuesday, "tuesday": time.Tuesday,
	"wed": time.Wednesday, "weds": time.Wednesday, "wednesday": time.Wednesday,
	"thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday, "thursday": time.Thursday,
	"fri": time.Friday, "friday": time.Friday,
	"sat": time.Saturday, "saturday": time.Saturday,
}

// parseRelative covers everything that isn't strict ISO. Returns the
// parsed time or an error. Anchored to `now`; result is in `now`'s tz.
func parseRelative(raw string, now time.Time) (time.Time, error) {
	t := strings.ToLower(strings.TrimSpace(raw))

	// Split into (datePart, timePart). The time part may be: "HH:MM",
	// "<H>am", "<H>pm", "<H>:MMam", "<H>:MMpm". Search for it from the
	// right so multi-word date phrases ("next monday") stay intact.
	datePart, timePart := splitDateTime(t)

	if timePart != "" {
		// Strip the leading "at " — semantically a no-op modifier on top of
		// a bare time ("at 3pm" = today at 3pm, with forward-date if past).
		bareDate := datePart
		if bareDate == "at" || bareDate == "" {
			bareDate = ""
		}

		var d time.Time
		var ok bool
		if bareDate == "" {
			d, ok = now, true
		} else {
			d, ok = resolveDatePhrase(bareDate, now)
		}
		if !ok {
			return time.Time{}, fmt.Errorf("could not resolve %q", raw)
		}
		h, m, terr := parseTimeOfDay(timePart)
		if terr != nil {
			return time.Time{}, terr
		}
		result := time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, now.Location())
		// "at 10am" / bare time → forward-date when today's time is past.
		if bareDate == "" && result.Before(now) {
			result = result.AddDate(0, 0, 1)
		}
		return result, nil
	}

	// No explicit time component.
	//
	// "in N <unit>" phrases carry an implicit time (hours/minutes shift,
	// or N-days-from-now preserves now's HH:MM). Don't zero those.
	if strings.HasPrefix(t, "in ") {
		d, ok := parseInPhrase(strings.TrimPrefix(t, "in "), now)
		if !ok {
			return time.Time{}, fmt.Errorf("could not resolve %q", raw)
		}
		return d, nil
	}

	// Date-only phrase ("tomorrow", "next friday", "end of week", weekday) →
	// midnight in now's tz.
	d, ok := resolveDatePhrase(t, now)
	if !ok {
		return time.Time{}, fmt.Errorf("could not resolve %q", raw)
	}
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, now.Location()), nil
}

// splitDateTime finds an "HH:MM" or "<N>am|pm" at the end of the string
// and returns (datePart, timePart). Both lowercased.
func splitDateTime(s string) (string, string) {
	// Try regex from the right.
	// Time patterns we accept anywhere at end: "13:45", "1pm", "1:30pm", etc.
	timeRe := regexp.MustCompile(`(?:^|\s)(\d{1,2}:\d{2}|\d{1,2}(?::\d{2})?\s*(?:am|pm))$`)
	loc := timeRe.FindStringIndex(s)
	if loc == nil {
		return strings.TrimSpace(s), ""
	}
	timePart := strings.TrimSpace(s[loc[0]:loc[1]])
	datePart := strings.TrimSpace(s[:loc[0]])
	return datePart, timePart
}

func parseTimeOfDay(t string) (hour, minute int, err error) {
	t = strings.ReplaceAll(strings.ToLower(t), " ", "")
	if strings.HasSuffix(t, "am") || strings.HasSuffix(t, "pm") {
		ampm := t[len(t)-2:]
		body := t[:len(t)-2]
		if strings.Contains(body, ":") {
			parts := strings.SplitN(body, ":", 2)
			h, herr := strconv.Atoi(parts[0])
			m, merr := strconv.Atoi(parts[1])
			if herr != nil || merr != nil {
				return 0, 0, fmt.Errorf("bad time %q", t)
			}
			hour, minute = h, m
		} else {
			h, herr := strconv.Atoi(body)
			if herr != nil {
				return 0, 0, fmt.Errorf("bad time %q", t)
			}
			hour, minute = h, 0
		}
		if hour == 12 {
			hour = 0
		}
		if ampm == "pm" {
			hour += 12
		}
		return hour, minute, nil
	}
	// 24-hour HH:MM
	parts := strings.SplitN(t, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("bad time %q", t)
	}
	h, herr := strconv.Atoi(parts[0])
	m, merr := strconv.Atoi(parts[1])
	if herr != nil || merr != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("bad time %q", t)
	}
	return h, m, nil
}

// resolveDatePhrase handles every supported relative phrase.
// Returns (date, true) on success; second is false on no match.
func resolveDatePhrase(phrase string, now time.Time) (time.Time, bool) {
	switch phrase {
	case "":
		return now, true
	case "today":
		return now, true
	case "tomorrow":
		return now.AddDate(0, 0, 1), true
	case "yesterday":
		return now.AddDate(0, 0, -1), true
	case "next week":
		return nextWeekdayFrom(now.AddDate(0, 0, 7), time.Monday, true), true
	case "end of week":
		// Friday this week. If today is Sat/Sun, take next Friday.
		return nextWeekdayFrom(now, time.Friday, false), true
	case "end of month":
		first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		return first.AddDate(0, 1, -1), true
	}

	// "next <weekday>"
	if strings.HasPrefix(phrase, "next ") {
		wd, ok := weekdayMap[strings.TrimPrefix(phrase, "next ")]
		if ok {
			return nextWeekdayFrom(now.AddDate(0, 0, 1), wd, true), true
		}
	}
	// "this <weekday>"
	if strings.HasPrefix(phrase, "this ") {
		wd, ok := weekdayMap[strings.TrimPrefix(phrase, "this ")]
		if ok {
			return weekdayInWeek(now, wd), true
		}
	}
	// "in N <unit>"
	if strings.HasPrefix(phrase, "in ") {
		rest := strings.TrimPrefix(phrase, "in ")
		if d, ok := parseInPhrase(rest, now); ok {
			return d, true
		}
	}
	// Bare weekday → forward-date (next occurrence including today if forward-only,
	// or strictly future if today=that weekday).
	if wd, ok := weekdayMap[phrase]; ok {
		return nextWeekdayFrom(now, wd, true), true
	}
	return time.Time{}, false
}

// nextWeekdayFrom returns the next occurrence of `wd` at or after `from`.
// If `strict` is true, "today is wd" yields next week's same weekday.
func nextWeekdayFrom(from time.Time, wd time.Weekday, strict bool) time.Time {
	diff := int(wd - from.Weekday())
	if diff < 0 {
		diff += 7
	}
	if diff == 0 && strict {
		diff = 7
	}
	return from.AddDate(0, 0, diff)
}

// weekdayInWeek returns the date in the current week (Mon-Sun) for the
// given weekday. "Current week" = Mon to Sun containing `now`.
func weekdayInWeek(now time.Time, wd time.Weekday) time.Time {
	// Find Monday of this week.
	wkdDiff := int(now.Weekday()-time.Monday+7) % 7
	monday := now.AddDate(0, 0, -wkdDiff)
	return monday.AddDate(0, 0, int(wd-time.Monday+7)%7)
}

var inPhraseRe = regexp.MustCompile(`^(\d+)\s+(minute|min|hour|hr|day|week|month)s?$`)

func parseInPhrase(s string, now time.Time) (time.Time, bool) {
	m := inPhraseRe.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return time.Time{}, false
	}
	switch m[2] {
	case "minute", "min":
		return now.Add(time.Duration(n) * time.Minute), true
	case "hour", "hr":
		return now.Add(time.Duration(n) * time.Hour), true
	case "day":
		return now.AddDate(0, 0, n), true
	case "week":
		return now.AddDate(0, 0, n*7), true
	case "month":
		return now.AddDate(0, n, 0), true
	}
	return time.Time{}, false
}

// ─── Formatters ────────────────────────────────────────────────────────

func formatLocalDateTime(t time.Time) string {
	return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d",
		t.Year(), int(t.Month()), t.Day(),
		t.Hour(), t.Minute(), t.Second())
}

func formatLocalDate(t time.Time) string {
	return fmt.Sprintf("%04d-%02d-%02d", t.Year(), int(t.Month()), t.Day())
}
