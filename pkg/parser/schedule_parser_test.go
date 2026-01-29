//go:build !integration

package parser

import (
	"testing"
)

func TestParseSchedule(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedCron   string
		expectedOrig   string
		shouldError    bool
		errorSubstring string
	}{
		// Daily schedules
		{
			name:         "daily default time",
			input:        "daily",
			expectedCron: "FUZZY:DAILY * * *",
			expectedOrig: "daily",
		},
		{
			name:           "daily at 02:00",
			input:          "daily at 02:00",
			shouldError:    true,
			errorSubstring: "'daily at <time>' syntax is not supported",
		},
		{
			name:           "daily at midnight",
			input:          "daily at midnight",
			shouldError:    true,
			errorSubstring: "'daily at <time>' syntax is not supported",
		},
		{
			name:           "daily at noon",
			input:          "daily at noon",
			shouldError:    true,
			errorSubstring: "'daily at <time>' syntax is not supported",
		},
		{
			name:           "daily at 3pm",
			input:          "daily at 3pm",
			shouldError:    true,
			errorSubstring: "'daily at <time>' syntax is not supported",
		},
		{
			name:           "daily at 1am",
			input:          "daily at 1am",
			shouldError:    true,
			errorSubstring: "'daily at <time>' syntax is not supported",
		},
		{
			name:           "daily at 12am (midnight)",
			input:          "daily at 12am",
			shouldError:    true,
			errorSubstring: "'daily at <time>' syntax is not supported",
		},
		{
			name:           "daily at 12pm (noon)",
			input:          "daily at 12pm",
			shouldError:    true,
			errorSubstring: "'daily at <time>' syntax is not supported",
		},
		{
			name:           "daily at 11pm",
			input:          "daily at 11pm",
			shouldError:    true,
			errorSubstring: "'daily at <time>' syntax is not supported",
		},
		{
			name:           "daily at 6am",
			input:          "daily at 6am",
			shouldError:    true,
			errorSubstring: "'daily at <time>' syntax is not supported",
		},

		// Daily around schedules (fuzzy with target time)
		{
			name:         "daily around 02:00",
			input:        "daily around 02:00",
			expectedCron: "FUZZY:DAILY_AROUND:2:0 * * *",
			expectedOrig: "daily around 02:00",
		},
		{
			name:         "daily around midnight",
			input:        "daily around midnight",
			expectedCron: "FUZZY:DAILY_AROUND:0:0 * * *",
			expectedOrig: "daily around midnight",
		},
		{
			name:         "daily around noon",
			input:        "daily around noon",
			expectedCron: "FUZZY:DAILY_AROUND:12:0 * * *",
			expectedOrig: "daily around noon",
		},
		{
			name:         "daily around 3pm",
			input:        "daily around 3pm",
			expectedCron: "FUZZY:DAILY_AROUND:15:0 * * *",
			expectedOrig: "daily around 3pm",
		},
		{
			name:         "daily around 14:30",
			input:        "daily around 14:30",
			expectedCron: "FUZZY:DAILY_AROUND:14:30 * * *",
			expectedOrig: "daily around 14:30",
		},
		{
			name:         "daily around 9am",
			input:        "daily around 9am",
			expectedCron: "FUZZY:DAILY_AROUND:9:0 * * *",
			expectedOrig: "daily around 9am",
		},
		{
			name:         "daily around 14:00 utc+9",
			input:        "daily around 14:00 utc+9",
			expectedCron: "FUZZY:DAILY_AROUND:5:0 * * *",
			expectedOrig: "daily around 14:00 utc+9",
		},
		{
			name:         "daily around 3pm utc-5",
			input:        "daily around 3pm utc-5",
			expectedCron: "FUZZY:DAILY_AROUND:20:0 * * *",
			expectedOrig: "daily around 3pm utc-5",
		},
		{
			name:         "daily around 9am utc+05:30",
			input:        "daily around 9am utc+05:30",
			expectedCron: "FUZZY:DAILY_AROUND:3:30 * * *",
			expectedOrig: "daily around 9am utc+05:30",
		},
		{
			name:         "daily around midnight utc-8",
			input:        "daily around midnight utc-8",
			expectedCron: "FUZZY:DAILY_AROUND:8:0 * * *",
			expectedOrig: "daily around midnight utc-8",
		},
		{
			name:         "daily around noon utc+2",
			input:        "daily around noon utc+2",
			expectedCron: "FUZZY:DAILY_AROUND:10:0 * * *",
			expectedOrig: "daily around noon utc+2",
		},

		// Daily between schedules (fuzzy with time range)
		{
			name:         "daily between 9:00 and 17:00",
			input:        "daily between 9:00 and 17:00",
			expectedCron: "FUZZY:DAILY_BETWEEN:9:0:17:0 * * *",
			expectedOrig: "daily between 9:00 and 17:00",
		},
		{
			name:         "daily between 9am and 5pm",
			input:        "daily between 9am and 5pm",
			expectedCron: "FUZZY:DAILY_BETWEEN:9:0:17:0 * * *",
			expectedOrig: "daily between 9am and 5pm",
		},
		{
			name:         "daily between midnight and noon",
			input:        "daily between midnight and noon",
			expectedCron: "FUZZY:DAILY_BETWEEN:0:0:12:0 * * *",
			expectedOrig: "daily between midnight and noon",
		},
		{
			name:         "daily between noon and midnight",
			input:        "daily between noon and midnight",
			expectedCron: "FUZZY:DAILY_BETWEEN:12:0:0:0 * * *",
			expectedOrig: "daily between noon and midnight",
		},
		{
			name:         "daily between 22:00 and 02:00",
			input:        "daily between 22:00 and 02:00",
			expectedCron: "FUZZY:DAILY_BETWEEN:22:0:2:0 * * *",
			expectedOrig: "daily between 22:00 and 02:00",
		},
		{
			name:         "daily between 10pm and 2am",
			input:        "daily between 10pm and 2am",
			expectedCron: "FUZZY:DAILY_BETWEEN:22:0:2:0 * * *",
			expectedOrig: "daily between 10pm and 2am",
		},
		{
			name:         "daily between 8:30 and 18:45",
			input:        "daily between 8:30 and 18:45",
			expectedCron: "FUZZY:DAILY_BETWEEN:8:30:18:45 * * *",
			expectedOrig: "daily between 8:30 and 18:45",
		},
		{
			name:         "daily between 9am utc-5 and 5pm utc-5",
			input:        "daily between 9am utc-5 and 5pm utc-5",
			expectedCron: "FUZZY:DAILY_BETWEEN:14:0:22:0 * * *",
			expectedOrig: "daily between 9am utc-5 and 5pm utc-5",
		},
		{
			name:         "daily between 8:00 utc+9 and 17:00 utc+9",
			input:        "daily between 8:00 utc+9 and 17:00 utc+9",
			expectedCron: "FUZZY:DAILY_BETWEEN:23:0:8:0 * * *",
			expectedOrig: "daily between 8:00 utc+9 and 17:00 utc+9",
		},
		{
			name:         "daily between 6am and 6pm",
			input:        "daily between 6am and 6pm",
			expectedCron: "FUZZY:DAILY_BETWEEN:6:0:18:0 * * *",
			expectedOrig: "daily between 6am and 6pm",
		},
		{
			name:         "daily between 1am and 11pm",
			input:        "daily between 1am and 11pm",
			expectedCron: "FUZZY:DAILY_BETWEEN:1:0:23:0 * * *",
			expectedOrig: "daily between 1am and 11pm",
		},
		{
			name:         "daily between 00:00 and 23:59",
			input:        "daily between 00:00 and 23:59",
			expectedCron: "FUZZY:DAILY_BETWEEN:0:0:23:59 * * *",
			expectedOrig: "daily between 00:00 and 23:59",
		},
		{
			name:         "daily between 12am and 11:59pm",
			input:        "daily between 12am and 23:59",
			expectedCron: "FUZZY:DAILY_BETWEEN:0:0:23:59 * * *",
			expectedOrig: "daily between 12am and 23:59",
		},
		{
			name:         "daily between 23:00 and 01:00",
			input:        "daily between 23:00 and 01:00",
			expectedCron: "FUZZY:DAILY_BETWEEN:23:0:1:0 * * *",
			expectedOrig: "daily between 23:00 and 01:00",
		},
		{
			name:         "daily between 11pm and 1am",
			input:        "daily between 11pm and 1am",
			expectedCron: "FUZZY:DAILY_BETWEEN:23:0:1:0 * * *",
			expectedOrig: "daily between 11pm and 1am",
		},
		{
			name:         "daily between 7:15 and 16:45",
			input:        "daily between 7:15 and 16:45",
			expectedCron: "FUZZY:DAILY_BETWEEN:7:15:16:45 * * *",
			expectedOrig: "daily between 7:15 and 16:45",
		},
		{
			name:         "daily between 3:30am and 8:30pm",
			input:        "daily between 3:30 and 20:30",
			expectedCron: "FUZZY:DAILY_BETWEEN:3:30:20:30 * * *",
			expectedOrig: "daily between 3:30 and 20:30",
		},
		{
			name:         "daily between noon and 6pm",
			input:        "daily between noon and 6pm",
			expectedCron: "FUZZY:DAILY_BETWEEN:12:0:18:0 * * *",
			expectedOrig: "daily between noon and 6pm",
		},
		{
			name:         "daily between midnight and 6am",
			input:        "daily between midnight and 6am",
			expectedCron: "FUZZY:DAILY_BETWEEN:0:0:6:0 * * *",
			expectedOrig: "daily between midnight and 6am",
		},
		{
			name:         "daily between 6pm and midnight",
			input:        "daily between 6pm and midnight",
			expectedCron: "FUZZY:DAILY_BETWEEN:18:0:0:0 * * *",
			expectedOrig: "daily between 6pm and midnight",
		},
		{
			name:         "daily between 10:00 utc+0 and 14:00 utc+0",
			input:        "daily between 10:00 utc+0 and 14:00 utc+0",
			expectedCron: "FUZZY:DAILY_BETWEEN:10:0:14:0 * * *",
			expectedOrig: "daily between 10:00 utc+0 and 14:00 utc+0",
		},
		{
			name:         "daily between 9am utc-8 and 5pm utc-8",
			input:        "daily between 9am utc-8 and 5pm utc-8",
			expectedCron: "FUZZY:DAILY_BETWEEN:17:0:1:0 * * *",
			expectedOrig: "daily between 9am utc-8 and 5pm utc-8",
		},
		{
			name:         "daily between 8:00 utc+05:30 and 18:00 utc+05:30",
			input:        "daily between 8:00 utc+05:30 and 18:00 utc+05:30",
			expectedCron: "FUZZY:DAILY_BETWEEN:2:30:12:30 * * *",
			expectedOrig: "daily between 8:00 utc+05:30 and 18:00 utc+05:30",
		},

		// Daily between error cases
		{
			name:           "daily between missing and",
			input:          "daily between 9:00 17:00 extra",
			shouldError:    true,
			errorSubstring: "missing 'and' keyword",
		},
		{
			name:           "daily between missing end time",
			input:          "daily between 9:00 and",
			shouldError:    true,
			errorSubstring: "invalid 'between' format",
		},
		{
			name:           "daily between same time",
			input:          "daily between 9:00 and 9:00",
			shouldError:    true,
			errorSubstring: "start and end times cannot be the same",
		},
		{
			name:           "daily between incomplete",
			input:          "daily between 9:00",
			shouldError:    true,
			errorSubstring: "invalid 'between' format",
		},
		{
			name:         "daily between invalid start time",
			input:        "daily between 25:00 and 17:00",
			shouldError:  false, // parseTime returns 0:0 for invalid times
			expectedCron: "FUZZY:DAILY_BETWEEN:0:0:17:0 * * *",
			expectedOrig: "daily between 25:00 and 17:00",
		},
		{
			name:         "daily between invalid end time",
			input:        "daily between 9:00 and 25:00",
			shouldError:  false, // parseTime returns 0:0 for invalid times
			expectedCron: "FUZZY:DAILY_BETWEEN:9:0:0:0 * * *",
			expectedOrig: "daily between 9:00 and 25:00",
		},
		{
			name:           "daily between with only 'and'",
			input:          "daily between and",
			shouldError:    true,
			errorSubstring: "invalid 'between' format",
		},
		{
			name:           "daily between missing both times",
			input:          "daily between and and",
			shouldError:    true,
			errorSubstring: "invalid 'between' format",
		},
		{
			name:           "daily between same time at midnight",
			input:          "daily between midnight and midnight",
			shouldError:    true,
			errorSubstring: "start and end times cannot be the same",
		},
		{
			name:           "daily between same time at noon",
			input:          "daily between noon and noon",
			shouldError:    true,
			errorSubstring: "start and end times cannot be the same",
		},
		{
			name:           "daily between same time with am/pm",
			input:          "daily between 3pm and 15:00",
			shouldError:    true,
			errorSubstring: "start and end times cannot be the same",
		},

		// Hourly schedules
		{
			name:         "hourly",
			input:        "hourly",
			expectedCron: "FUZZY:HOURLY/1 * * *",
			expectedOrig: "hourly",
		},

		// Weekly schedules (fuzzy)
		{
			name:         "weekly fuzzy",
			input:        "weekly",
			expectedCron: "FUZZY:WEEKLY * * *",
			expectedOrig: "weekly",
		},
		{
			name:         "weekly on monday fuzzy",
			input:        "weekly on monday",
			expectedCron: "FUZZY:WEEKLY:1 * * *",
			expectedOrig: "weekly on monday",
		},
		{
			name:         "weekly on sunday fuzzy",
			input:        "weekly on sunday",
			expectedCron: "FUZZY:WEEKLY:0 * * *",
			expectedOrig: "weekly on sunday",
		},
		{
			name:         "weekly on friday fuzzy",
			input:        "weekly on friday",
			expectedCron: "FUZZY:WEEKLY:5 * * *",
			expectedOrig: "weekly on friday",
		},
		{
			name:         "weekly on saturday fuzzy",
			input:        "weekly on saturday",
			expectedCron: "FUZZY:WEEKLY:6 * * *",
			expectedOrig: "weekly on saturday",
		},
		{
			name:         "weekly on tuesday fuzzy",
			input:        "weekly on tuesday",
			expectedCron: "FUZZY:WEEKLY:2 * * *",
			expectedOrig: "weekly on tuesday",
		},

		// Weekly schedules (fixed time) - now rejected
		{
			name:           "weekly on monday at 06:30",
			input:          "weekly on monday at 06:30",
			shouldError:    true,
			errorSubstring: "'weekly on <weekday> at <time>' syntax is not supported",
		},
		{
			name:           "weekly on friday at 17:00",
			input:          "weekly on friday at 17:00",
			shouldError:    true,
			errorSubstring: "'weekly on <weekday> at <time>' syntax is not supported",
		},
		{
			name:           "weekly on saturday at midnight",
			input:          "weekly on saturday at midnight",
			shouldError:    true,
			errorSubstring: "'weekly on <weekday> at <time>' syntax is not supported",
		},
		{
			name:           "daily at 02:00 utc+9",
			input:          "daily at 02:00 utc+9",
			shouldError:    true,
			errorSubstring: "'daily at <time>' syntax is not supported",
		},
		{
			name:           "daily at 14:00 utc-5",
			input:          "daily at 14:00 utc-5",
			shouldError:    true,
			errorSubstring: "'daily at <time>' syntax is not supported",
		},
		{
			name:           "daily at 09:30 utc+05:30",
			input:          "daily at 09:30 utc+05:30",
			shouldError:    true,
			errorSubstring: "'daily at <time>' syntax is not supported",
		},
		{
			name:           "weekly on monday at 08:00 utc+0",
			input:          "weekly on monday at 08:00 utc+0",
			shouldError:    true,
			errorSubstring: "'weekly on <weekday> at <time>' syntax is not supported",
		},
		{
			name:           "monthly on 15 at 12:00 utc-8",
			input:          "monthly on 15 at 12:00 utc-8",
			shouldError:    true,
			errorSubstring: "'monthly on <day> at <time>' syntax is not supported",
		},
		{
			name:           "daily at 3pm utc+9",
			input:          "daily at 3pm utc+9",
			shouldError:    true,
			errorSubstring: "'daily at <time>' syntax is not supported",
		},
		{
			name:           "daily at 9am utc-5",
			input:          "daily at 9am utc-5",
			shouldError:    true,
			errorSubstring: "'daily at <time>' syntax is not supported",
		},
		{
			name:           "daily at 12pm utc+1",
			input:          "daily at 12pm utc+1",
			shouldError:    true,
			errorSubstring: "'daily at <time>' syntax is not supported",
		},
		{
			name:           "daily at 12am utc-8",
			input:          "daily at 12am utc-8",
			shouldError:    true,
			errorSubstring: "'daily at <time>' syntax is not supported",
		},
		{
			name:           "daily at 11pm utc+05:30",
			input:          "daily at 11pm utc+05:30",
			shouldError:    true,
			errorSubstring: "'daily at <time>' syntax is not supported",
		},
		{
			name:           "weekly on monday at 8am utc+9",
			input:          "weekly on monday at 8am utc+9",
			shouldError:    true,
			errorSubstring: "'weekly on <weekday> at <time>' syntax is not supported",
		},
		{
			name:           "weekly on friday at 6pm utc-7",
			input:          "weekly on friday at 6pm utc-7",
			shouldError:    true,
			errorSubstring: "'weekly on <weekday> at <time>' syntax is not supported",
		},

		// Weekly around schedules (fuzzy with target time)
		{
			name:         "weekly on monday around 09:00",
			input:        "weekly on monday around 09:00",
			expectedCron: "FUZZY:WEEKLY_AROUND:1:9:0 * * *",
			expectedOrig: "weekly on monday around 09:00",
		},
		{
			name:         "weekly on friday around 17:00",
			input:        "weekly on friday around 17:00",
			expectedCron: "FUZZY:WEEKLY_AROUND:5:17:0 * * *",
			expectedOrig: "weekly on friday around 17:00",
		},
		{
			name:         "weekly on sunday around midnight",
			input:        "weekly on sunday around midnight",
			expectedCron: "FUZZY:WEEKLY_AROUND:0:0:0 * * *",
			expectedOrig: "weekly on sunday around midnight",
		},
		{
			name:         "weekly on wednesday around noon",
			input:        "weekly on wednesday around noon",
			expectedCron: "FUZZY:WEEKLY_AROUND:3:12:0 * * *",
			expectedOrig: "weekly on wednesday around noon",
		},
		{
			name:         "weekly on thursday around 14:30",
			input:        "weekly on thursday around 14:30",
			expectedCron: "FUZZY:WEEKLY_AROUND:4:14:30 * * *",
			expectedOrig: "weekly on thursday around 14:30",
		},
		{
			name:         "weekly on saturday around 9am",
			input:        "weekly on saturday around 9am",
			expectedCron: "FUZZY:WEEKLY_AROUND:6:9:0 * * *",
			expectedOrig: "weekly on saturday around 9am",
		},
		{
			name:         "weekly on tuesday around 3pm",
			input:        "weekly on tuesday around 3pm",
			expectedCron: "FUZZY:WEEKLY_AROUND:2:15:0 * * *",
			expectedOrig: "weekly on tuesday around 3pm",
		},
		{
			name:         "weekly on monday around 08:00 utc+9",
			input:        "weekly on monday around 08:00 utc+9",
			expectedCron: "FUZZY:WEEKLY_AROUND:1:23:0 * * *",
			expectedOrig: "weekly on monday around 08:00 utc+9",
		},
		{
			name:         "weekly on friday around 5pm utc-5",
			input:        "weekly on friday around 5pm utc-5",
			expectedCron: "FUZZY:WEEKLY_AROUND:5:22:0 * * *",
			expectedOrig: "weekly on friday around 5pm utc-5",
		},
		{
			name:         "weekly on friday around 8 am PT",
			input:        "weekly on friday around 8 am PT",
			expectedCron: "FUZZY:WEEKLY_AROUND:5:16:0 * * *",
			expectedOrig: "weekly on friday around 8 am PT",
		},
		{
			name:           "monthly on 15 at 10am utc+2",
			input:          "monthly on 15 at 10am utc+2",
			shouldError:    true,
			errorSubstring: "'monthly on <day> at <time>' syntax is not supported",
		},
		{
			name:           "monthly on 1 at 7pm utc-3",
			input:          "monthly on 1 at 7pm utc-3",
			shouldError:    true,
			errorSubstring: "'monthly on <day> at <time>' syntax is not supported",
		},
		{
			name:           "weekly on friday at 5pm",
			input:          "weekly on friday at 5pm",
			shouldError:    true,
			errorSubstring: "'weekly on <weekday> at <time>' syntax is not supported",
		},
		{
			name:           "monthly on 15 at 9am",
			input:          "monthly on 15 at 9am",
			shouldError:    true,
			errorSubstring: "'monthly on <day> at <time>' syntax is not supported",
		},

		// Monthly schedules - now rejected
		{
			name:           "monthly on 1st",
			input:          "monthly on 1",
			shouldError:    true,
			errorSubstring: "'monthly on <day>' syntax is not supported",
		},
		{
			name:           "monthly on 15th",
			input:          "monthly on 15",
			shouldError:    true,
			errorSubstring: "'monthly on <day>' syntax is not supported",
		},
		{
			name:           "monthly on 15th at 09:00",
			input:          "monthly on 15 at 09:00",
			shouldError:    true,
			errorSubstring: "'monthly on <day> at <time>' syntax is not supported",
		},
		{
			name:           "monthly on 31st",
			input:          "monthly on 31",
			shouldError:    true,
			errorSubstring: "'monthly on <day>' syntax is not supported",
		},

		// Bi-weekly schedules (fuzzy)
		{
			name:         "bi-weekly fuzzy",
			input:        "bi-weekly",
			expectedCron: "FUZZY:BI_WEEKLY * * *",
			expectedOrig: "bi-weekly",
		},
		{
			name:           "bi-weekly with parameters",
			input:          "bi-weekly on monday",
			shouldError:    true,
			errorSubstring: "bi-weekly schedule does not support additional parameters",
		},

		// Tri-weekly schedules (fuzzy)
		{
			name:         "tri-weekly fuzzy",
			input:        "tri-weekly",
			expectedCron: "FUZZY:TRI_WEEKLY * * *",
			expectedOrig: "tri-weekly",
		},
		{
			name:           "tri-weekly with parameters",
			input:          "tri-weekly on friday",
			shouldError:    true,
			errorSubstring: "tri-weekly schedule does not support additional parameters",
		},

		// Interval schedules
		{
			name:         "every 10 minutes",
			input:        "every 10 minutes",
			expectedCron: "*/10 * * * *",
			expectedOrig: "every 10 minutes",
		},
		{
			name:         "every 5 minutes",
			input:        "every 5 minutes",
			expectedCron: "*/5 * * * *",
			expectedOrig: "every 5 minutes",
		},
		{
			name:         "every 30 minutes",
			input:        "every 30 minutes",
			expectedCron: "*/30 * * * *",
			expectedOrig: "every 30 minutes",
		},
		{
			name:         "every 1 hour",
			input:        "every 1 hour",
			expectedCron: "FUZZY:HOURLY/1 * * *",
			expectedOrig: "every 1 hour",
		},
		{
			name:         "every 2 hours",
			input:        "every 2 hours",
			expectedCron: "FUZZY:HOURLY/2 * * *",
			expectedOrig: "every 2 hours",
		},
		{
			name:         "every 6 hours",
			input:        "every 6 hours",
			expectedCron: "FUZZY:HOURLY/6 * * *",
			expectedOrig: "every 6 hours",
		},
		{
			name:         "every 12 hours",
			input:        "every 12 hours",
			expectedCron: "FUZZY:HOURLY/12 * * *",
			expectedOrig: "every 12 hours",
		},

		// Short duration formats (like stop-after)
		{
			name:         "every 30m",
			input:        "every 30m",
			expectedCron: "*/30 * * * *",
			expectedOrig: "every 30m",
		},
		{
			name:         "every 1h",
			input:        "every 1h",
			expectedCron: "FUZZY:HOURLY/1 * * *",
			expectedOrig: "every 1h",
		},
		{
			name:         "every 2h",
			input:        "every 2h",
			expectedCron: "FUZZY:HOURLY/2 * * *",
			expectedOrig: "every 2h",
		},
		{
			name:         "every 6h",
			input:        "every 6h",
			expectedCron: "FUZZY:HOURLY/6 * * *",
			expectedOrig: "every 6h",
		},
		{
			name:         "every 1d",
			input:        "every 1d",
			expectedCron: "0 0 * * *",
			expectedOrig: "every 1d",
		},
		{
			name:         "every 2d",
			input:        "every 2d",
			expectedCron: "0 0 */2 * *",
			expectedOrig: "every 2d",
		},
		{
			name:         "every 1w",
			input:        "every 1w",
			expectedCron: "0 0 * * 0",
			expectedOrig: "every 1w",
		},
		{
			name:         "every 2w",
			input:        "every 2w",
			expectedCron: "0 0 */14 * *",
			expectedOrig: "every 2w",
		},
		{
			name:         "every 1mo",
			input:        "every 1mo",
			expectedCron: "0 0 1 * *",
			expectedOrig: "every 1mo",
		},
		{
			name:         "every 2mo",
			input:        "every 2mo",
			expectedCron: "0 0 1 */2 *",
			expectedOrig: "every 2mo",
		},

		// Case insensitivity
		{
			name:         "DAILY uppercase",
			input:        "DAILY",
			expectedCron: "FUZZY:DAILY * * *",
			expectedOrig: "DAILY",
		},
		{
			name:         "Weekly On Monday mixed case",
			input:        "Weekly On Monday",
			expectedCron: "FUZZY:WEEKLY:1 * * *",
			expectedOrig: "Weekly On Monday",
		},

		// Already cron expressions (should pass through)
		{
			name:         "existing cron expression",
			input:        "0 9 * * 1",
			expectedCron: "0 9 * * 1",
			expectedOrig: "",
		},
		{
			name:         "complex cron expression",
			input:        "*/15 * * * *",
			expectedCron: "*/15 * * * *",
			expectedOrig: "",
		},
		{
			name:         "cron with ranges",
			input:        "0 14 * * 1-5",
			expectedCron: "0 14 * * 1-5",
			expectedOrig: "",
		},

		// Error cases
		{
			name:           "empty string",
			input:          "",
			shouldError:    true,
			errorSubstring: "cannot be empty",
		},
		{
			name:           "interval with time conflict",
			input:          "every 10 minutes at 06:00",
			shouldError:    true,
			errorSubstring: "cannot have 'at time'",
		},
		{
			name:           "invalid interval number",
			input:          "every abc minutes",
			shouldError:    true,
			errorSubstring: "invalid interval",
		},
		{
			name:         "every 2 days",
			input:        "every 2 days",
			expectedCron: "0 0 */2 * *",
			expectedOrig: "every 2 days",
		},
		{
			name:         "every 3 days",
			input:        "every 3 days",
			expectedCron: "0 0 */3 * *",
			expectedOrig: "every 3 days",
		},
		{
			name:         "every 7 days",
			input:        "every 7 days",
			expectedCron: "0 0 */7 * *",
			expectedOrig: "every 7 days",
		},
		{
			name:         "every 10 days",
			input:        "every 10 days",
			expectedCron: "0 0 */10 * *",
			expectedOrig: "every 10 days",
		},
		{
			name:         "every 14 days",
			input:        "every 14 days",
			expectedCron: "0 0 */14 * *",
			expectedOrig: "every 14 days",
		},
		{
			name:         "every 1 day",
			input:        "every 1 day",
			expectedCron: "0 0 * * *",
			expectedOrig: "every 1 day",
		},
		{
			name:           "weekly without on",
			input:          "weekly monday",
			shouldError:    true,
			errorSubstring: "requires 'on <weekday>'",
		},
		{
			name:           "weekly invalid weekday",
			input:          "weekly on funday",
			shouldError:    true,
			errorSubstring: "invalid weekday",
		},
		{
			name:           "monthly without on",
			input:          "monthly 15",
			shouldError:    true,
			errorSubstring: "requires 'on <day>'",
		},
		{
			name:           "monthly invalid day",
			input:          "monthly on 32",
			shouldError:    true,
			errorSubstring: "invalid day of month",
		},
		{
			name:           "monthly day out of range",
			input:          "monthly on 0",
			shouldError:    true,
			errorSubstring: "invalid day of month",
		},
		{
			name:           "negative interval",
			input:          "every -5 minutes",
			shouldError:    true,
			errorSubstring: "invalid interval",
		},
		{
			name:           "zero interval",
			input:          "every 0 minutes",
			shouldError:    true,
			errorSubstring: "invalid interval",
		},
		// Minimum duration validation (5 minutes)
		{
			name:           "interval below minimum - 1m",
			input:          "every 1m",
			shouldError:    true,
			errorSubstring: "minimum schedule interval is 5 minutes",
		},
		{
			name:           "interval below minimum - 2 minutes",
			input:          "every 2 minutes",
			shouldError:    true,
			errorSubstring: "minimum schedule interval is 5 minutes",
		},
		{
			name:           "interval below minimum - 4m",
			input:          "every 4m",
			shouldError:    true,
			errorSubstring: "minimum schedule interval is 5 minutes",
		},
		{
			name:         "interval at minimum - 5m",
			input:        "every 5m",
			expectedCron: "*/5 * * * *",
			expectedOrig: "every 5m",
		},
		{
			name:         "interval at minimum - 5 minutes",
			input:        "every 5 minutes",
			expectedCron: "*/5 * * * *",
			expectedOrig: "every 5 minutes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cron, orig, err := ParseSchedule(tt.input)

			if tt.shouldError {
				if err == nil {
					t.Errorf("expected error containing '%s', got nil", tt.errorSubstring)
					return
				}
				if tt.errorSubstring != "" && !containsSubstring(err.Error(), tt.errorSubstring) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errorSubstring, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if cron != tt.expectedCron {
				t.Errorf("expected cron '%s', got '%s'", tt.expectedCron, cron)
			}

			if orig != tt.expectedOrig {
				t.Errorf("expected original '%s', got '%s'", tt.expectedOrig, orig)
			}
		})
	}
}

// containsSubstring checks if s contains substr (case-insensitive)
func containsSubstring(s, substr string) bool {
	return len(substr) == 0 || len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsIgnoreCase(s, substr))
}

func containsIgnoreCase(s, substr string) bool {
	s = toLower(s)
	substr = toLower(substr)
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
