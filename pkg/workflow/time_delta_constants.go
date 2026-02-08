package workflow

// Time delta validation limits
//
// Policy: Maximum stop-after time is 1 year to prevent scheduling too far in the future.
// These constants define the maximum allowed values for each time unit when parsing
// time deltas in workflow schedules. The limits ensure workflows don't schedule actions
// unreasonably far into the future, which could indicate configuration errors or create
// operational challenges.
//
// All limits are equivalent to approximately 1 year:
//   - 12 months = 1 year (exact)
//   - 52 weeks = 364 days ≈ 1 year
//   - 365 days = 1 year (non-leap year)
//   - 8760 hours = 365 days * 24 hours
//   - 525600 minutes = 365 days * 24 hours * 60 minutes
const (
	// MaxTimeDeltaMonths is the maximum allowed months in a time delta (1 year)
	MaxTimeDeltaMonths = 12

	// MaxTimeDeltaWeeks is the maximum allowed weeks in a time delta (approximately 1 year)
	MaxTimeDeltaWeeks = 52

	// MaxTimeDeltaDays is the maximum allowed days in a time delta (1 year, non-leap)
	MaxTimeDeltaDays = 365

	// MaxTimeDeltaHours is the maximum allowed hours in a time delta (365 days * 24 hours)
	MaxTimeDeltaHours = 8760

	// MaxTimeDeltaMinutes is the maximum allowed minutes in a time delta (365 days * 24 hours * 60 minutes)
	MaxTimeDeltaMinutes = 525600
)
