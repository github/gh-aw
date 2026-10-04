package timenowsub

import clock "time"

func badAlias(t clock.Time) {
	_ = clock.Now().Sub(t) // want `clock\.Now\(\)\.Sub\(t\) can be simplified to clock\.Since\(t\)`
}

func badParenthesizedAlias(t clock.Time) {
	_ = (clock.Now()).Sub(t) // want `clock\.Now\(\)\.Sub\(t\) can be simplified to clock\.Since\(t\)`
}
