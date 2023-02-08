package actors

import (
	"fmt"
	"github.com/pkg/errors"
	"strconv"
	"time"
)

func parseISO8601Duration(from string) (time.Duration, int, error) {
	match := pattern.FindStringSubmatch(from)
	if match == nil {
		return 0, 0, errors.Errorf("unsupported ISO8601 duration format %q", from)
	}
	duration := time.Duration(0)
	// -1 signifies infinite repetition
	repetition := -1
	for i, name := range pattern.SubexpNames() {
		part := match[i]
		if i == 0 || name == "" || part == "" {
			continue
		}
		val, err := strconv.Atoi(part)
		if err != nil {
			return 0, 0, err
		}
		switch name {
		case "year":
			duration += time.Hour * 24 * 365 * time.Duration(val)
		case "month":
			duration += time.Hour * 24 * 30 * time.Duration(val)
		case "week":
			duration += time.Hour * 24 * 7 * time.Duration(val)
		case "day":
			duration += time.Hour * 24 * time.Duration(val)
		case "hour":
			duration += time.Hour * time.Duration(val)
		case "minute":
			duration += time.Minute * time.Duration(val)
		case "second":
			duration += time.Second * time.Duration(val)
		case "repetition":
			repetition = val
		default:
			return 0, 0, fmt.Errorf("unsupported ISO8601 duration field %s", name)
		}
	}
	return duration, repetition, nil
}

// parseDuration creates time.Duration from either:
// - ISO8601 duration format,
// - time.Duration string format.
func parseDuration(from string) (time.Duration, int, error) {
	d, r, err := parseISO8601Duration(from)
	if err == nil {
		return d, r, nil
	}
	d, err = time.ParseDuration(from)
	if err == nil {
		return d, -1, nil
	}
	return 0, 0, errors.Errorf("unsupported duration format %q", from)
}

// parseTime creates time.Duration from either:
// - ISO8601 duration format,
// - time.Duration string format,
// - RFC3339 datetime format.
// For duration formats, an offset is added.
func parseTime(from string, offset *time.Time) (time.Time, error) {
	var start time.Time
	if offset != nil {
		start = *offset
	} else {
		start = time.Now()
	}
	d, r, err := parseISO8601Duration(from)
	if err == nil {
		if r != -1 {
			return time.Time{}, errors.Errorf("repetitions are not allowed")
		}
		return start.Add(d), nil
	}
	if d, err = time.ParseDuration(from); err == nil {
		return start.Add(d), nil
	}
	if t, err := time.Parse(time.RFC3339, from); err == nil {
		return t, nil
	}
	return time.Time{}, errors.Errorf("unsupported time/duration format %q", from)
}

func GetParseTime(from string, offset *time.Time) (time.Time, error) {
	return parseTime(from, offset)
}
