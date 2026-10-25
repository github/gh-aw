//go:build !integration

package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScheduleParserTimeHelperBounds(t *testing.T) {
	helpers := []struct {
		name    string
		extract func(*ScheduleParser, int) (string, error)
	}{
		{"time", (*ScheduleParser).extractTime},
		{"between", func(p *ScheduleParser, start int) (string, error) {
			return p.extractTimeBetween(start, len(p.tokens))
		}},
		{"after", func(p *ScheduleParser, start int) (string, error) {
			return p.extractTimeAfter(start, false)
		}},
		{"after weekdays", func(p *ScheduleParser, start int) (string, error) {
			return p.extractTimeAfter(start, true)
		}},
		{"with weekdays disabled", func(p *ScheduleParser, start int) (string, error) {
			return p.extractTimeWithWeekdays(start, false)
		}},
		{"with weekdays", func(p *ScheduleParser, start int) (string, error) {
			return p.extractTimeWithWeekdays(start, true)
		}},
	}
	tests := []struct {
		name   string
		tokens []string
		start  int
		want   string
	}{
		{"empty negative", nil, -1, ""},
		{"empty zero", nil, 0, ""},
		{"negative", []string{"noon", "on", "weekdays"}, -1, ""},
		{"equal to length", []string{"noon", "on", "weekdays"}, 3, ""},
		{"beyond length", []string{"noon", "on", "weekdays"}, 4, ""},
		{"valid", []string{"noon", "on", "weekdays"}, 0, "noon"},
	}
	for _, helper := range helpers {
		t.Run(helper.name, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					p := &ScheduleParser{tokens: tt.tokens}
					got, err := helper.extract(p, tt.start)
					if tt.want == "" {
						require.EqualError(t, err, "expected time specification")
					} else {
						require.NoError(t, err)
					}
					require.Equal(t, tt.want, got)
				})
			}
		})
	}
}

func TestScheduleParserTimeBetweenEndBounds(t *testing.T) {
	p := &ScheduleParser{tokens: []string{"noon"}}
	for _, end := range []int{-1, 0} {
		got, err := p.extractTimeBetween(0, end)
		require.EqualError(t, err, "expected time specification")
		require.Empty(t, got)
	}
	for _, end := range []int{1, 2} {
		got, err := p.extractTimeBetween(0, end)
		require.NoError(t, err)
		require.Equal(t, "noon", got)
	}
}

func TestScheduleParserIntervalHelperBounds(t *testing.T) {
	for _, tokens := range [][]string{nil, {}, {"every"}} {
		p := &ScheduleParser{tokens: tokens}
		for _, weekdays := range []bool{false, true} {
			cron, handled, err := p.parseEveryDayIntervalAlias(weekdays)
			require.NoError(t, err)
			require.False(t, handled)
			require.Empty(t, cron)

			cron, handled, err = p.parseShortDurationInterval(weekdays)
			require.NoError(t, err)
			require.False(t, handled)
			require.Empty(t, cron)
		}
	}
	for _, tokens := range [][]string{nil, {}, {"every"}, {"every", "2"}} {
		p := &ScheduleParser{tokens: tokens}
		interval, unit, err := p.parseLongIntervalParts()
		require.ErrorContains(t, err, "invalid interval format")
		require.Zero(t, interval)
		require.Empty(t, unit)
	}

	p := &ScheduleParser{tokens: []string{"every", "2", "hours"}}
	interval, unit, err := p.parseLongIntervalParts()
	require.NoError(t, err)
	require.Equal(t, 2, interval)
	require.Equal(t, "hours", unit)
}

func TestScheduleParserIntervalTimeClauseBounds(t *testing.T) {
	for _, tokens := range [][]string{nil, {"at"}} {
		p := &ScheduleParser{tokens: tokens}
		for _, weekdays := range []bool{false, true} {
			require.EqualError(t, p.validateIntervalTimeClause(-1, weekdays), "invalid interval time clause start position")
			require.NoError(t, p.validateIntervalTimeClause(len(tokens), weekdays))
			require.NoError(t, p.validateIntervalTimeClause(len(tokens)+1, weekdays))
		}
	}
	p := &ScheduleParser{tokens: []string{"every", "2h", "at", "noon"}}
	require.EqualError(t, p.validateIntervalTimeClause(2, false), "interval schedules cannot have 'at time' clause")
	require.NoError(t, p.validateIntervalTimeClause(3, false))
}

func TestScheduleParserDailyHelperBounds(t *testing.T) {
	for _, tokens := range [][]string{nil, {}} {
		p := &ScheduleParser{tokens: tokens}
		for _, weekdays := range []bool{false, true} {
			cron, err := p.parseDailyBase(weekdays)
			require.EqualError(t, err, "empty schedule")
			require.Empty(t, cron)
		}
	}
	p := &ScheduleParser{tokens: []string{"daily"}}
	cron, err := p.parseDailyBase(false)
	require.NoError(t, err)
	require.Equal(t, "FUZZY:DAILY * * *", cron)
}
